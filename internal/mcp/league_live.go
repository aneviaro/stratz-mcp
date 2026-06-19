package mcp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/leaguelive"
)

func registerLeagueLiveHandlers(handlers map[string]ToolHandler, options Options, service *leaguelive.Service) {
	if handlers["stratz_get_league"] == nil {
		handlers["stratz_get_league"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			result, err := service.GetLeague(ctx, object["league_id"].(string))
			if err != nil {
				return nil, leagueLiveExecutionError(err)
			}
			return leagueLiveEnvelope(options, "get_league", detailInput(object), result, includeRaw(object), nil), nil
		}
	}
	if handlers["stratz_list_leagues"] == nil {
		handlers["stratz_list_leagues"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			filters, err := decodeLeagueFilters(object)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.ListLeagues(ctx, filters)
			if domainErr != nil {
				return nil, leagueLiveExecutionError(domainErr)
			}
			return leagueLiveEnvelope(options, "list_leagues", "", result, includeRaw(object), dateRange(filters.From, filters.To)), nil
		}
	}
	if handlers["stratz_list_league_matches"] == nil {
		handlers["stratz_list_league_matches"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			filters, err := decodeLeagueMatchFilters(object)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.ListLeagueMatches(ctx, filters)
			if domainErr != nil {
				return nil, leagueLiveExecutionError(domainErr)
			}
			return leagueLiveEnvelope(options, "list_league_matches", detailInput(object), result, includeRaw(object), dateRange(filters.From, filters.To)), nil
		}
	}
	if handlers["stratz_list_live_matches"] == nil {
		handlers["stratz_list_live_matches"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			filters, err := decodeLiveFilters(object)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.ListLiveMatches(ctx, filters)
			if domainErr != nil {
				return nil, leagueLiveExecutionError(domainErr)
			}
			return leagueLiveEnvelope(options, "list_live_matches", "", result, includeRaw(object), nil), nil
		}
	}
}

func leagueLiveEnvelope[T any](options Options, operation string, detail contracts.DetailLevel, result *leaguelive.Result[T], includeRaw bool, dates map[string]any) map[string]any {
	output := curatedEnvelope(options, operation, detail, result.Data, result.Raw, includeRaw, result.RateLimits, dates)
	warnings := make([]string, len(result.Warnings))
	copy(warnings, result.Warnings)
	output["warnings"] = warnings
	return output
}

func decodeLeagueFilters(input map[string]any) (leaguelive.LeagueFilters, error) {
	var filters leaguelive.LeagueFilters
	if err := decodeListFields(input, &filters.Limit, &filters.Cursor, &filters.From, &filters.To); err != nil {
		return filters, err
	}
	for key, destination := range map[string]**string{"query": &filters.Query, "status": &filters.Status, "tier": &filters.Tier} {
		if value, ok := input[key].(string); ok {
			copy := value
			*destination = &copy
		}
	}
	return filters, nil
}

func decodeLeagueMatchFilters(input map[string]any) (leaguelive.LeagueMatchFilters, error) {
	filters := leaguelive.LeagueMatchFilters{LeagueID: input["league_id"].(string)}
	if err := decodeListFields(input, &filters.Limit, &filters.Cursor, &filters.From, &filters.To); err != nil {
		return filters, err
	}
	if value, ok := input["patch_id"].(string); ok {
		filters.PatchID = &value
	}
	return filters, nil
}

func decodeLiveFilters(input map[string]any) (leaguelive.LiveFilters, error) {
	var filters leaguelive.LiveFilters
	if value, ok := input["limit"]; ok {
		number, valid := rawInteger(value)
		if !valid {
			return filters, invalidArgumentsError()
		}
		filters.Limit = int(number)
	}
	filters.Cursor, _ = input["cursor"].(string)
	filters.Sort, _ = input["sort"].(string)
	for key, destination := range map[string]**int64{
		"game_mode_id": &filters.GameModeID, "minimum_spectators": &filters.MinimumSpectators,
	} {
		if value, ok := input[key]; ok {
			number, valid := rawInteger(value)
			if !valid {
				return filters, invalidArgumentsError()
			}
			*destination = &number
		}
	}
	for key, destination := range map[string]**int64{
		"player_id": &filters.PlayerID, "team_id": &filters.TeamID, "league_id": &filters.LeagueID, "hero": &filters.HeroID,
	} {
		if value, ok := input[key]; ok {
			number, valid := identifierInteger(value)
			if !valid || number < 1 {
				return filters, &ExecutionError{
					Code: contracts.ErrorCodeInvalidArgument, Message: key + " must be a positive numeric identifier",
					Details: map[string]any{}, Retryable: false,
				}
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

func decodeListFields(input map[string]any, limit *int, cursor *string, from, to **time.Time) error {
	if value, ok := input["limit"]; ok {
		number, valid := rawInteger(value)
		if !valid {
			return invalidArgumentsError()
		}
		*limit = int(number)
	}
	*cursor, _ = input["cursor"].(string)
	for key, destination := range map[string]**time.Time{"from": from, "to": to} {
		if value, ok := input[key].(string); ok {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return invalidArgumentsError()
			}
			*destination = &parsed
		}
	}
	return nil
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

func dateRange(from, to *time.Time) map[string]any {
	if from == nil && to == nil {
		return nil
	}
	result := map[string]any{"from": nil, "to": nil}
	if from != nil {
		result["from"] = from.UTC().Format(time.RFC3339)
	}
	if to != nil {
		result["to"] = to.UTC().Format(time.RFC3339)
	}
	return result
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
