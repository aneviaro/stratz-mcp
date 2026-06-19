package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
)

func registerHeroConstantsHandlers(
	handlers map[string]ToolHandler,
	options Options,
	service *heroconstants.Service,
) {
	if handlers["stratz_get_hero"] == nil {
		handlers["stratz_get_hero"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			result, err := service.GetHero(ctx, object["hero"])
			if err != nil {
				return nil, heroConstantsExecutionError(err)
			}
			return heroConstantsEnvelope(options, "get_hero", detailInput(object), result, includeRaw(object)), nil
		}
	}
	if handlers["stratz_batch_get_heroes"] == nil {
		handlers["stratz_batch_get_heroes"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			identifiers, ok := object["heroes"].([]any)
			if !ok {
				return nil, invalidArgumentsError()
			}
			result, err := service.BatchHeroes(ctx, identifiers)
			if err != nil {
				return nil, heroConstantsExecutionError(err)
			}
			data := contracts.StratzBatchGetHeroesData{Items: result.Data}
			wrapped := &heroconstants.Result[contracts.StratzBatchGetHeroesData]{
				Data: data, Raw: result.Raw, RateLimits: result.RateLimits, Warnings: result.Warnings,
			}
			return heroConstantsEnvelope(options, "batch_get_heroes", detailInput(object), wrapped, includeRaw(object)), nil
		}
	}
	if handlers["stratz_get_constants"] == nil {
		handlers["stratz_get_constants"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			result, err := service.GetConstants(ctx, object["type"].(string))
			if err != nil {
				return nil, heroConstantsExecutionError(err)
			}
			return heroConstantsEnvelope(options, "get_constants", "", result, includeRaw(object)), nil
		}
	}
	if handlers["stratz_get_hero_stats"] == nil {
		handlers["stratz_get_hero_stats"] = func(ctx context.Context, input any) (any, error) {
			object := input.(map[string]any)
			filters, err := decodeHeroStatsFilters(object)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.GetHeroStats(ctx, filters)
			if domainErr != nil {
				return nil, heroConstantsExecutionError(domainErr)
			}
			return heroConstantsEnvelope(options, "get_hero_stats", "", result, includeRaw(object)), nil
		}
	}
}

func heroConstantsEnvelope[T any](
	options Options,
	operation string,
	detail contracts.DetailLevel,
	result *heroconstants.Result[T],
	includeRaw bool,
) map[string]any {
	var dateRange map[string]any
	if result.EffectiveRange != nil {
		dateRange = map[string]any{
			"from": result.EffectiveRange.From.UTC().Format(time.RFC3339),
			"to":   result.EffectiveRange.To.UTC().Format(time.RFC3339),
		}
	}
	output := curatedEnvelope(
		options,
		operation,
		detail,
		result.Data,
		result.Raw,
		includeRaw,
		result.RateLimits,
		dateRange,
	)
	warnings := make([]string, len(result.Warnings))
	copy(warnings, result.Warnings)
	output["warnings"] = warnings
	if result.PatchID != nil {
		output["provenance"].(map[string]any)["patch"] = map[string]any{
			"id": *result.PatchID, "name": nil,
		}
	}
	return output
}

func decodeHeroStatsFilters(input map[string]any) (heroconstants.StatsFilters, error) {
	filters := heroconstants.StatsFilters{Hero: input["hero"]}
	for key, destination := range map[string]**time.Time{
		"from": &filters.From,
		"to":   &filters.To,
	} {
		if value, ok := input[key].(string); ok {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return filters, invalidArgumentsError()
			}
			*destination = &parsed
		}
	}
	for key, destination := range map[string]**string{
		"patch_id":     &filters.PatchID,
		"rank_bracket": &filters.RankBracket,
		"role":         &filters.Role,
		"lane":         &filters.Lane,
	} {
		if value, ok := input[key].(string); ok {
			copy := value
			*destination = &copy
		}
	}
	filters.IncludeMatchups, _ = input["include_matchups"].(bool)
	filters.IncludeSynergies, _ = input["include_synergies"].(bool)
	return filters, nil
}

func heroConstantsExecutionError(err error) error {
	var domainErr *heroconstants.Error
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
		RetryAfter: retryAfter, Details: domainErr.Details, FailedInput: domainErr.FailedInput,
	}
}
