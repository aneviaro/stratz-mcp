package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
)

// registerHeroConstantsHandlers registers the two v2 query adapters owned by
// the hero/constants domain. Input shape and mode validation are authoritative
// in the generated contract and the domain operation respectively; this layer
// only forwards the validated request and builds the public envelope.
func registerHeroConstantsHandlers(
	handlers map[string]ToolHandler,
	options Options,
	service *heroconstants.Service,
) {
	if handlers["stratz_query_heroes"] == nil {
		handlers["stratz_query_heroes"] = func(ctx context.Context, input any) (any, error) {
			object, err := inputObject(input)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.QueryHeroes(ctx, contracts.StratzQueryHeroesRequest(object))
			if domainErr != nil {
				return nil, heroConstantsExecutionError(domainErr)
			}
			return heroConstantsEnvelope(options, "query_heroes", "", result, includeRaw(object)), nil
		}
	}
	if handlers["stratz_query_constants"] == nil {
		handlers["stratz_query_constants"] = func(ctx context.Context, input any) (any, error) {
			object, err := inputObject(input)
			if err != nil {
				return nil, err
			}
			result, domainErr := service.QueryConstants(ctx, contracts.StratzQueryConstantsRequest(object))
			if domainErr != nil {
				return nil, heroConstantsExecutionError(domainErr)
			}
			return heroConstantsEnvelope(options, "query_constants", "", result, includeRaw(object)), nil
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
