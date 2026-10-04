package mcp

import (
	"context"
	"encoding/json"
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
	_ *heroconstants.Service,
) {
	if handlers["stratz_query_players"] == nil {
		handlers["stratz_query_players"] = func(ctx context.Context, input any) (any, error) {
			object, err := inputObject(input)
			if err != nil {
				return nil, err
			}
			request, err := decodePlayerQueryRequest(object)
			if err != nil {
				return nil, invalidArgumentsError()
			}
			result, domainErr := service.QueryPlayers(ctx, request)
			if domainErr != nil {
				return nil, playerMatchExecutionError(domainErr)
			}
			data := map[string]any{
				"mode":  result.Data.Mode,
				"items": result.Data.Items,
			}
			return curatedEnvelope(options, "query_players", "", data, result.Raw, includeRaw(object), result.RateLimits, nil), nil
		}
	}
}

func decodePlayerQueryRequest(input map[string]any) (contracts.StratzQueryPlayersRequest, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return contracts.StratzQueryPlayersRequest{}, err
	}
	var request contracts.StratzQueryPlayersRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return contracts.StratzQueryPlayersRequest{}, err
	}
	return request, nil
}

func decodeQueryPlayerHistoryFilters(
	ctx context.Context,
	input map[string]any,
	heroes *heroconstants.Service,
	budget *stratz.RequestBudget,
) (playermatch.PlayerMatchFilters, error) {
	player, ok := input["player_id"].(string)
	if !ok || player == "" {
		return playermatch.PlayerMatchFilters{}, invalidArgumentsError()
	}
	filters := playermatch.PlayerMatchFilters{PlayerID: player}
	detail := detailInput(input)
	if detail != contracts.DetailLevelSummary && detail != contracts.DetailLevel("players") {
		return filters, invalidArgumentsError()
	}
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
	if detail == contracts.DetailLevel("players") {
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
		"role": &filters.Role, "result": &filters.Result, "patch_id": &filters.PatchID,
	} {
		if value, present := input[key].(string); present {
			copy := value
			*destination = &copy
		}
	}
	for key, destination := range map[string]**time.Time{"from": &filters.From, "to": &filters.To} {
		if value, present := input[key].(string); present {
			parsed, parseErr := time.Parse(time.RFC3339, value)
			if parseErr != nil {
				return filters, invalidArgumentsError()
			}
			*destination = &parsed
		}
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
	return filters, nil
}

func queryMatchesEnvelope(
	options Options,
	resultData any,
	raw any,
	warnings []string,
	rates []stratz.RateLimit,
	includeRaw bool,
	detail contracts.DetailLevel,
) map[string]any {
	output := curatedEnvelope(options, "query_matches", detail, resultData, raw, includeRaw, rates, nil)
	copiedWarnings := make([]string, len(warnings))
	copy(copiedWarnings, warnings)
	output["warnings"] = copiedWarnings
	return output
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
	mode, _ := input["mode"].(string)
	if mode == "live" {
		return ""
	}
	if value, ok := input["detail_level"].(string); ok {
		return contracts.DetailLevel(value)
	}
	switch mode {
	case "player_history", "league_history":
		return contracts.DetailLevelSummary
	default:
		return contracts.DetailLevelStandard
	}
}

func matchDetailInput(input map[string]any) contracts.DetailLevel {
	return detailInput(input)
}

func rejectPlayersDetail(input map[string]any) error {
	if detailInput(input) != contracts.DetailLevel("players") {
		return nil
	}
	mode, _ := input["mode"].(string)
	if mode != "exact" && mode != "player_history" {
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
