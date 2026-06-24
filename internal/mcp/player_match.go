package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
	"github.com/aneviaro/stratz-mcp/internal/domain/playermatch"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

func registerPlayerMatchHandlers(
	handlers map[string]ToolHandler,
	options Options,
	service *playermatch.Service,
	heroes *heroconstants.Service,
) {
	if handlers["stratz_get_player"] == nil {
		handlers["stratz_get_player"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			if err := rejectPlayersDetail(object); err != nil {
				return nil, err
			}
			result, err := service.GetPlayer(ctx, object["player_id"].(string))
			if err != nil {
				return nil, playerMatchExecutionError(err)
			}
			return curatedEnvelope(options, "get_player", detailInput(object), result.Data, result.Raw, includeRaw(object), result.RateLimits, nil), nil
		}
	}
	if handlers["stratz_batch_get_players"] == nil {
		handlers["stratz_batch_get_players"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			if err := rejectPlayersDetail(object); err != nil {
				return nil, err
			}
			identifiers, err := stringSlice(object["player_ids"])
			if err != nil {
				return nil, invalidArgumentsError()
			}
			result, domainErr := service.BatchPlayers(ctx, identifiers)
			if domainErr != nil {
				return nil, playerMatchExecutionError(domainErr)
			}
			data := contracts.StratzBatchGetPlayersData{Items: result.Data}
			return curatedEnvelope(options, "batch_get_players", detailInput(object), data, result.Raw, includeRaw(object), result.RateLimits, nil), nil
		}
	}
	if handlers["stratz_get_match"] == nil {
		handlers["stratz_get_match"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			result, err := service.GetMatch(ctx, object["match_id"].(string), detailInput(object))
			if err != nil {
				return nil, playerMatchExecutionError(err)
			}
			output := curatedEnvelope(options, "get_match", detailInput(object), result.Data, result.Raw, includeRaw(object), result.RateLimits, nil)
			output["warnings"] = result.Warnings
			return output, nil
		}
	}
	if handlers["stratz_batch_get_matches"] == nil {
		handlers["stratz_batch_get_matches"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			identifiers, err := stringSlice(object["match_ids"])
			if err != nil {
				return nil, invalidArgumentsError()
			}
			result, domainErr := service.BatchMatches(ctx, identifiers, detailInput(object))
			if domainErr != nil {
				return nil, playerMatchExecutionError(domainErr)
			}
			data := contracts.StratzBatchGetMatchesData{Items: result.Data}
			output := curatedEnvelope(options, "batch_get_matches", detailInput(object), data, result.Raw, includeRaw(object), result.RateLimits, nil)
			output["warnings"] = result.Warnings
			return output, nil
		}
	}
	if handlers["stratz_list_player_matches"] == nil {
		handlers["stratz_list_player_matches"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			budget, _ := stratz.NewRequestBudget(options.Config.Limits.MaxUpstreamRequests)
			filters, err := decodePlayerMatchFilters(ctx, object, heroes, budget)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.ListPlayerMatchesWithBudget(ctx, filters, budget)
			if domainErr != nil {
				return nil, playerMatchExecutionError(domainErr)
			}
			dateRange := map[string]any{"from": nil, "to": nil}
			if filters.From != nil {
				dateRange["from"] = filters.From.UTC().Format(time.RFC3339)
			}
			if filters.To != nil {
				dateRange["to"] = filters.To.UTC().Format(time.RFC3339)
			}
			if filters.From == nil && filters.To == nil {
				dateRange = nil
			}
			output := curatedEnvelope(options, "list_player_matches", detailInput(object), result.Data, result.Raw, includeRaw(object), result.RateLimits, dateRange)
			output["warnings"] = result.Warnings
			return output, nil
		}
	}
}

func curatedEnvelope(
	options Options,
	operation string,
	detail contracts.DetailLevel,
	data any,
	raw any,
	includeRaw bool,
	rates []stratz.RateLimit,
	dateRange map[string]any,
) map[string]any {
	var publicDetail any
	if detail != "" {
		publicDetail = detail
	}
	output := map[string]any{
		"kind":    "success",
		"data":    data,
		"summary": nil,
		"provenance": map[string]any{
			"retrieved_at":   options.Now().UTC().Format(time.RFC3339),
			"operation":      operation,
			"schema_version": options.SchemaVersion,
			"detail_level":   publicDetail,
			"cache": map[string]any{
				"status":      "miss",
				"age_seconds": nil,
			},
			"patch":       nil,
			"date_range":  dateRange,
			"rate_limits": publicRateLimits(rates),
		},
		"warnings": []string{},
	}
	if includeRaw {
		output["raw"] = raw
	}
	return output
}

func playerMatchExecutionError(err error) error {
	var domainErr *playermatch.Error
	if !errors.As(err, &domainErr) {
		return err
	}
	var retryAfter *string
	if domainErr.RetryAfter != nil {
		value := domainErr.RetryAfter.UTC().Format(time.RFC3339)
		retryAfter = &value
	}
	return &ExecutionError{
		Code:        domainErr.Code,
		Message:     domainErr.Message,
		Retryable:   domainErr.Retryable,
		RetryAfter:  retryAfter,
		Details:     domainErr.Details,
		FailedInput: domainErr.FailedInput,
		Context:     domainErr.Context,
	}
}

func detailInput(input map[string]any) contracts.DetailLevel {
	if value, ok := input["detail_level"].(string); ok {
		return contracts.DetailLevel(value)
	}
	return contracts.DetailLevelStandard
}

func rejectPlayersDetail(input map[string]any) error {
	if detailInput(input) == contracts.DetailLevel("players") {
		return invalidArgumentsError()
	}
	return nil
}

func includeRaw(input map[string]any) bool {
	value, _ := input["include_raw"].(bool)
	return value
}

func stringSlice(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("not an array")
	}
	result := make([]string, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, errors.New("not a string array")
		}
		result[index] = text
	}
	return result, nil
}

func decodePlayerMatchFilters(
	ctx context.Context,
	input map[string]any,
	heroes *heroconstants.Service,
	budget *stratz.RequestBudget,
) (playermatch.PlayerMatchFilters, error) {
	filters := playermatch.PlayerMatchFilters{PlayerID: input["player_id"].(string)}
	if value, ok := input["limit"]; ok {
		number, valid := rawInteger(value)
		if !valid {
			return filters, invalidArgumentsError()
		}
		filters.Limit = int(number)
	}
	if value, ok := input["cursor"].(string); ok {
		filters.Cursor = value
	}
	if value, ok := input["include_player"].(bool); ok {
		filters.IncludePlayer = value
	}
	if detailInput(input) == contracts.DetailLevel("players") {
		filters.IncludePlayer = true
	}
	for key, destination := range map[string]**int64{
		"game_mode_id":             &filters.GameModeID,
		"lobby_type_id":            &filters.LobbyTypeID,
		"minimum_duration_seconds": &filters.MinimumDurationSeconds,
	} {
		if value, present := input[key]; present {
			number, valid := rawInteger(value)
			if !valid {
				return filters, invalidArgumentsError()
			}
			*destination = &number
		}
	}
	for key, destination := range map[string]**string{
		"role":     &filters.Role,
		"result":   &filters.Result,
		"patch_id": &filters.PatchID,
	} {
		if value, present := input[key].(string); present {
			copy := value
			*destination = &copy
		}
	}
	for key, destination := range map[string]**time.Time{
		"from": &filters.From,
		"to":   &filters.To,
	} {
		if value, present := input[key].(string); present {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return filters, invalidArgumentsError()
			}
			*destination = &parsed
		}
	}
	if hero, present := input["hero"]; present {
		number, resolveErr := heroes.ResolveHeroIDWithBudget(ctx, hero, budget)
		if resolveErr != nil {
			return filters, heroConstantsExecutionError(resolveErr)
		}
		filters.HeroID = &number
	}
	return filters, nil
}
