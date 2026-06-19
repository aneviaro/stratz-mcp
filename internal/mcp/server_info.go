package mcp

import (
	"context"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

func serverInfoHandler(options Options) ToolHandler {
	return func(ctx context.Context, _ any) (any, error) {
		health := stratz.Probe(ctx, options.Executor)
		cacheStatus := string(cache.StatusDisabled)
		if options.Config.Cache.Enabled {
			cacheStatus = string(options.CacheStatus)
			if cacheStatus == "" {
				cacheStatus = string(cache.StatusHealthy)
			}
		}
		warnings := []string{}
		if health.Status != stratz.HealthReachable {
			warnings = append(warnings, upstreamWarning(health.Status))
		}
		return map[string]any{
			"kind": "success",
			"data": map[string]any{
				"server_version":       options.Version,
				"mcp_protocol_version": contracts.MCPProtocolVersion,
				"schema_version":       options.SchemaVersion,
				"cache_status":         cacheStatus,
				"upstream_status":      health.Status,
				"limits":               publicLimits(options),
			},
			"summary": nil,
			"provenance": map[string]any{
				"retrieved_at":   options.Now().UTC().Format(time.RFC3339),
				"operation":      "server_info",
				"schema_version": options.SchemaVersion,
				"detail_level":   nil,
				"cache": map[string]any{
					"status":      "disabled",
					"age_seconds": nil,
				},
				"patch":       nil,
				"date_range":  nil,
				"rate_limits": publicRateLimits(health.RateLimits),
			},
			"warnings": warnings,
		}, nil
	}
}

func publicLimits(options Options) map[string]any {
	limits := options.Config.Limits
	return map[string]any{
		"upstream_timeout":            limits.UpstreamTimeout.String(),
		"max_response_bytes":          limits.MaxResponseBytes,
		"max_query_document_bytes":    limits.MaxQueryDocumentBytes,
		"max_query_variables_bytes":   limits.MaxQueryVariablesBytes,
		"max_query_depth":             limits.MaxQueryDepth,
		"max_query_aliases":           limits.MaxQueryAliases,
		"max_query_fields":            limits.MaxQueryFields,
		"max_query_complexity":        limits.MaxQueryComplexity,
		"max_list_page_size":          limits.MaxListPageSize,
		"max_graphql_operations":      limits.MaxGraphQLOperations,
		"max_upstream_requests":       limits.MaxUpstreamRequests,
		"max_batch_size":              limits.MaxBatchSize,
		"max_individual_string_bytes": limits.MaxIndividualStringSize,
		"cache_enabled":               options.Config.Cache.Enabled,
		"max_cache_size_bytes":        options.Config.Cache.MaxSizeBytes,
	}
}

func publicRateLimits(rateLimits []stratz.RateLimit) []any {
	result := make([]any, 0, len(rateLimits))
	for _, rateLimit := range rateLimits {
		var resetAt any
		if rateLimit.ResetAt != nil {
			resetAt = rateLimit.ResetAt.UTC().Format(time.RFC3339)
		}
		var source any
		if rateLimit.Source != "" {
			source = rateLimit.Source
		}
		result = append(result, map[string]any{
			"window":    rateLimit.Window,
			"limit":     rateLimit.Limit,
			"remaining": rateLimit.Remaining,
			"reset_at":  resetAt,
			"source":    source,
		})
	}
	return result
}

func upstreamWarning(status stratz.HealthStatus) string {
	switch status {
	case stratz.HealthAuthenticationFailed:
		return "STRATZ rejected the configured credential"
	case stratz.HealthWAFBlocked:
		return "A Cloudflare challenge blocked STRATZ access"
	case stratz.HealthUnchecked:
		return "STRATZ connectivity was not checked"
	default:
		return "STRATZ is currently unreachable"
	}
}
