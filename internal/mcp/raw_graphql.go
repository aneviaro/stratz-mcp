package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	rawgraphql "github.com/aneviaro/stratz-mcp/internal/graphql"
)

func rawGraphQLHandler(
	options Options,
	service *rawgraphql.RawService,
) ToolHandler {
	return func(ctx context.Context, input any) (any, error) {
		request, err := decodeRawGraphQLRequest(input)
		if err != nil {
			return nil, err
		}
		result, err := service.Execute(ctx, request)
		if err != nil {
			var rawErr *rawgraphql.Error
			if errors.As(err, &rawErr) {
				var retryAfter *string
				if rawErr.RetryAfter != nil {
					formatted := rawErr.RetryAfter.UTC().Format(time.RFC3339)
					retryAfter = &formatted
				}
				return nil, &ExecutionError{
					Code:       rawErr.Code,
					Message:    rawErr.Message,
					Retryable:  rawErr.Retryable,
					RetryAfter: retryAfter,
					Details:    rawErr.Details,
				}
			}
			return nil, err
		}
		warnings := []string{}
		if result.Partial {
			warnings = append(
				warnings,
				"STRATZ returned partial GraphQL data with execution errors",
			)
		}
		return map[string]any{
			"kind": "success",
			"data": map[string]any{
				"graphql": map[string]any{
					"data":       result.Data,
					"errors":     result.Errors,
					"extensions": result.Extensions,
				},
				"partial":     result.Partial,
				"http_status": result.HTTPStatus,
			},
			"summary": nil,
			"provenance": map[string]any{
				"retrieved_at":   options.Now().UTC().Format(time.RFC3339),
				"operation":      "execute_graphql",
				"schema_version": options.SchemaVersion,
				"detail_level":   nil,
				"cache": map[string]any{
					"status":      "disabled",
					"age_seconds": nil,
				},
				"patch":       nil,
				"date_range":  nil,
				"rate_limits": publicRateLimits(result.RateLimits),
			},
			"warnings": warnings,
		}, nil
	}
}

func decodeRawGraphQLRequest(input any) (rawgraphql.RawRequest, error) {
	object, ok := input.(map[string]any)
	if !ok {
		return rawgraphql.RawRequest{}, invalidArgumentsError()
	}
	query, ok := object["query"].(string)
	if !ok {
		return rawgraphql.RawRequest{}, invalidArgumentsError()
	}
	request := rawgraphql.RawRequest{
		Query: query,
	}
	if variables, present := object["variables"]; present {
		variablesObject, ok := variables.(map[string]any)
		if !ok {
			return rawgraphql.RawRequest{}, invalidArgumentsError()
		}
		request.Variables = variablesObject
	}
	if operationName, present := object["operation_name"]; present {
		value, ok := operationName.(string)
		if !ok {
			return rawgraphql.RawRequest{}, invalidArgumentsError()
		}
		request.OperationName = value
	}
	if cache, present := object["cache"]; present {
		value, ok := cache.(bool)
		if !ok {
			return rawgraphql.RawRequest{}, invalidArgumentsError()
		}
		request.Cache = value
	}
	if fresh, present := object["fresh"]; present {
		value, ok := fresh.(bool)
		if !ok {
			return rawgraphql.RawRequest{}, invalidArgumentsError()
		}
		request.Fresh = value
	}
	if ttl, present := object["cache_ttl_seconds"]; present {
		value, ok := rawInteger(ttl)
		if !ok {
			return rawgraphql.RawRequest{}, invalidArgumentsError()
		}
		request.CacheTTLSeconds = &value
	}
	return request, nil
}

func rawInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		return number, err == nil
	case float64:
		number := int64(typed)
		return number, float64(number) == typed
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}
