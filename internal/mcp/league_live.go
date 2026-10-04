package mcp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
	"github.com/aneviaro/stratz-mcp/internal/domain/leaguelive"
	"github.com/aneviaro/stratz-mcp/internal/domain/playermatch"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

// registerLeagueLiveHandlers registers league queries and the consolidated
// match dispatcher. The dispatcher is the sole MCP handler for
// stratz_query_matches; it routes each validated mode to its owning domain.
func registerLeagueLiveHandlers(
	handlers map[string]ToolHandler,
	options Options,
	service *leaguelive.Service,
	heroes *heroconstants.Service,
	playerServices ...*playermatch.Service,
) {
	if handlers["stratz_query_leagues"] == nil {
		handlers["stratz_query_leagues"] = func(ctx context.Context, input any) (any, error) {
			object, err := inputObject(input)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.QueryLeagues(ctx, contracts.StratzQueryLeaguesRequest(object))
			if domainErr != nil {
				return nil, leagueLiveExecutionError(domainErr)
			}
			data := map[string]any{
				"mode":  result.Data.Mode,
				"items": result.Data.Items,
			}
			if result.Data.Page != nil {
				data["page"] = result.Data.Page
			}
			return leagueLiveEnvelope(options, "query_leagues", "", result, includeRaw(object), nil, data), nil
		}
	}

	if len(playerServices) == 0 || playerServices[0] == nil {
		return
	}
	playerService := playerServices[0]
	if handlers["stratz_query_matches"] != nil {
		return
	}
	handlers["stratz_query_matches"] = func(ctx context.Context, input any) (any, error) {
		object, err := inputObject(input)
		if err != nil {
			return nil, err
		}
		mode, ok := object["mode"].(string)
		if !ok {
			return nil, invalidArgumentsError()
		}
		var (
			resultData any
			raw        any
			warnings   []string
			rates      []stratz.RateLimit
		)
		switch mode {
		case "exact":
			result, domainErr := playerService.QueryMatches(ctx, contracts.StratzQueryMatchesRequest(object))
			if domainErr != nil {
				return nil, playerMatchExecutionError(domainErr)
			}
			resultData, raw, warnings, rates = result.Data, result.Raw, result.Warnings, result.RateLimits
		case "player_history":
			budget, budgetErr := stratz.NewRequestBudget(options.Config.Limits.MaxUpstreamRequests)
			if budgetErr != nil {
				return nil, budgetErr
			}
			filters, filterErr := decodeQueryPlayerHistoryFilters(ctx, object, heroes, budget)
			if filterErr != nil {
				return nil, filterErr
			}
			result, domainErr := playerService.ListPlayerMatchesWithBudget(ctx, filters, budget)
			if domainErr != nil {
				return nil, playerMatchExecutionError(domainErr)
			}
			page := &contracts.Page{NextCursor: result.Data.Page.NextCursor, HasMore: result.Data.Page.HasMore}
			resultData = playermatch.MatchQueryData{Mode: mode, Items: result.Data.Items, Page: page}
			raw, warnings, rates = result.Raw, result.Warnings, result.RateLimits
		case "league_history":
			result, domainErr := service.QueryMatches(ctx, contracts.StratzQueryMatchesRequest(object))
			if domainErr != nil {
				return nil, leagueLiveExecutionError(domainErr)
			}
			resultData, raw, warnings, rates = result.Data, result.Raw, result.Warnings, result.RateLimits
		case "live":
			budget, budgetErr := stratz.NewRequestBudget(options.Config.Limits.MaxUpstreamRequests)
			if budgetErr != nil {
				return nil, budgetErr
			}
			filters, filterErr := decodeQueryLiveFilters(ctx, object, heroes, budget)
			if filterErr != nil {
				return nil, filterErr
			}
			result, domainErr := service.ListLiveMatchesWithBudget(ctx, filters, budget)
			if domainErr != nil {
				return nil, leagueLiveExecutionError(domainErr)
			}
			page := &contracts.Page{NextCursor: result.Data.Page.NextCursor, HasMore: result.Data.Page.HasMore}
			resultData = leaguelive.MatchQueryData{Mode: mode, Items: result.Data.Items, Page: page}
			raw, warnings, rates = result.Raw, result.Warnings, result.RateLimits
		default:
			return nil, invalidArgumentsError()
		}
		return queryMatchesEnvelope(options, resultData, raw, warnings, rates, includeRaw(object), matchDetailInput(object)), nil
	}
}

func decodeQueryLiveFilters(
	ctx context.Context,
	input map[string]any,
	heroes *heroconstants.Service,
	budget *stratz.RequestBudget,
) (leaguelive.LiveFilters, error) {
	filters := leaguelive.LiveFilters{}
	if value, present := input["limit"]; present {
		number, valid := rawInteger(value)
		if !valid {
			return filters, invalidArgumentsError()
		}
		filters.Limit = int(number)
	}
	if value, ok := input["cursor"].(string); ok {
		filters.Cursor = value
	}
	if value, ok := input["sort"].(string); ok {
		filters.Sort = value
	}
	for key, destination := range map[string]**int64{
		"game_mode_id": &filters.GameModeID, "minimum_spectators": &filters.MinimumSpectators,
	} {
		if value, present := input[key]; present {
			number, valid := rawInteger(value)
			if !valid {
				return filters, invalidArgumentsError()
			}
			*destination = &number
		}
	}
	if value, present := input["player_id"]; present {
		text, ok := value.(string)
		if !ok {
			return filters, invalidArgumentsError()
		}
		identifier, normalizeErr := playermatch.NormalizePlayerID(text)
		if normalizeErr != nil {
			return filters, playerMatchExecutionError(normalizeErr)
		}
		number := int64(identifier.AccountID)
		filters.PlayerID = &number
	}
	if value, present := input["hero"]; present {
		if heroes == nil {
			return filters, invalidArgumentsError()
		}
		number, resolveErr := heroes.ResolveHeroIDWithBudget(ctx, value, budget)
		if resolveErr != nil {
			return filters, heroConstantsExecutionError(resolveErr)
		}
		filters.HeroID = &number
	}
	for key, destination := range map[string]**int64{"team_id": &filters.TeamID, "league_id": &filters.LeagueID} {
		if value, present := input[key]; present {
			number, valid := identifierInteger(value)
			if !valid || number < 1 {
				return filters, invalidArgumentsError()
			}
			*destination = &number
		}
	}
	var err error
	if filters.GameStates, err = stringSliceOptional(input["game_states"]); err != nil {
		return filters, invalidArgumentsError()
	}
	if filters.Tiers, err = stringSliceOptional(input["tiers"]); err != nil {
		return filters, invalidArgumentsError()
	}
	return filters, nil
}

func identifierInteger(value any) (int64, bool) {
	if number, ok := rawInteger(value); ok {
		return number, true
	}
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	number, err := strconv.ParseInt(text, 10, 64)
	return number, err == nil
}

func stringSliceOptional(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	return stringSlice(value)
}

func leagueLiveEnvelope[T any](
	options Options,
	operation string,
	detail contracts.DetailLevel,
	result *leaguelive.Result[T],
	includeRaw bool,
	dates map[string]any,
	data ...any,
) map[string]any {
	value := any(result.Data)
	if len(data) > 0 {
		value = data[0]
	}
	output := curatedEnvelope(options, operation, detail, value, result.Raw, includeRaw, result.RateLimits, dates)
	warnings := make([]string, len(result.Warnings))
	copy(warnings, result.Warnings)
	output["warnings"] = warnings
	return output
}

func leagueLiveExecutionError(err error) error {
	var domainErr *leaguelive.Error
	if !errors.As(err, &domainErr) {
		return err
	}
	var retryAfter *string
	if domainErr.RetryAfter != nil {
		value := domainErr.RetryAfter.UTC().Format(time.RFC3339)
		retryAfter = &value
	}
	return &ExecutionError{
		Code: domainErr.Code, Message: domainErr.Message, Retryable: domainErr.Retryable,
		RetryAfter: retryAfter, Details: domainErr.Details,
	}
}
