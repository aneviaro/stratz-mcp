package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

type cacheSpecification struct {
	domain string
	class  cache.Class
}

// cacheSpecificationFor classifies a generated, schema-validated request. A
// consolidated tool does not have one cache class: its mode and opt-in fields
// are part of the cache policy as well as the canonical key.
func cacheSpecificationFor(name string, arguments map[string]any) (cacheSpecification, bool) {
	mode, _ := arguments["mode"].(string)
	switch name {
	case "stratz_query_heroes":
		if mode != "exact" && mode != "search" {
			return cacheSpecification{}, false
		}
		if includeStatistics, ok := arguments["include_statistics"].(bool); ok && includeStatistics {
			return cacheSpecification{domain: "heroes", class: cache.ClassPublicRecent}, true
		}
		if _, present := arguments["include_statistics"]; present {
			if _, ok := arguments["include_statistics"].(bool); !ok {
				return cacheSpecification{}, false
			}
		}
		return cacheSpecification{domain: "heroes", class: cache.ClassPublicReference}, true
	case "stratz_query_constants":
		if mode != "types" && mode != "typed_selectors" {
			return cacheSpecification{}, false
		}
		return cacheSpecification{domain: "constants", class: cache.ClassPublicReference}, true
	case "stratz_query_players":
		if mode != "exact" {
			return cacheSpecification{}, false
		}
		return cacheSpecification{domain: "players", class: cache.ClassProfileSensitive}, true
	case "stratz_query_leagues":
		switch mode {
		case "exact":
			return cacheSpecification{domain: "leagues", class: cache.ClassPublicReference}, true
		case "search":
			return cacheSpecification{domain: "leagues", class: cache.ClassPublicRecent}, true
		default:
			return cacheSpecification{}, false
		}
	case "stratz_query_matches":
		switch mode {
		case "exact", "league_history":
			return cacheSpecification{domain: "matches", class: cache.ClassPublicRecent}, true
		case "player_history":
			return cacheSpecification{domain: "matches", class: cache.ClassProfileSensitive}, true
		case "live":
			return cacheSpecification{domain: "matches", class: cache.ClassPublicLive}, true
		default:
			return cacheSpecification{}, false
		}
	default:
		return cacheSpecification{}, false
	}
}

func cachedToolHandler(
	options Options,
	name string,
	handler ToolHandler,
) ToolHandler {
	if handler == nil || options.Cache == nil {
		return handler
	}
	return func(ctx context.Context, input any) (any, error) {
		arguments, err := inputObject(input)
		if err != nil {
			return nil, err
		}
		includeRaw := includeRaw(arguments)
		if !detailAllowedForRequest(name, arguments) {
			return nil, invalidArgumentsError()
		}
		specification, classified := cacheSpecificationFor(name, arguments)
		if !classified {
			output, handlerErr := handler(ctx, input)
			if handlerErr == nil {
				status := cache.LookupDisabled
				if includeRaw {
					status = cache.LookupBypass
				}
				setCacheProvenance(output, status, 0)
			}
			return output, handlerErr
		}

		fresh, _ := arguments["fresh"].(bool)
		classification := cache.ResolveClassification(
			options.Config.Cache,
			options.Config.Features,
			specification.domain,
			specification.class,
			includeRaw,
		)
		key, keyErr := cache.CanonicalKey(cache.KeyInput{
			Namespace:     options.CacheNamespace,
			Domain:        specification.domain,
			Class:         specification.class,
			Operation:     name,
			Arguments:     cacheArguments(arguments),
			DetailLevel:   cacheDetailLevel(name, arguments),
			IncludeRaw:    includeRaw,
			SchemaVersion: options.SchemaVersion,
		})
		if keyErr != nil || !classification.Cacheable {
			output, handlerErr := handler(ctx, input)
			if handlerErr == nil {
				status := cache.LookupDisabled
				if includeRaw {
					status = cache.LookupBypass
				}
				setCacheProvenance(output, status, 0)
			}
			return output, handlerErr
		}

		lookup, lookupErr := options.Cache.Lookup(ctx, cache.LookupRequest{
			Key:   key,
			Fresh: fresh,
		})
		if lookupErr == nil && lookup.Status == cache.LookupHit {
			if output, ok := decodeCachedOutput(lookup.Payload); ok {
				setCacheProvenance(output, lookup.Status, lookup.Age)
				return output, nil
			}
		}

		output, handlerErr := handler(ctx, input)
		if handlerErr != nil && !fresh && staleFallbackAllowed(handlerErr) {
			stale, staleErr := options.Cache.Lookup(ctx, cache.LookupRequest{
				Key:        key,
				AllowStale: true,
			})
			if staleErr == nil && stale.Status == cache.LookupStale {
				if cached, ok := decodeCachedOutput(stale.Payload); ok {
					setCacheProvenance(cached, stale.Status, stale.Age)
					appendWarning(cached, "Serving stale cached data because STRATZ was unavailable")
					return cached, nil
				}
			}
			return nil, handlerErr
		}
		if handlerErr != nil {
			return nil, handlerErr
		}

		status := lookup.Status
		if lookupErr != nil {
			status = cache.LookupDisabled
		}
		setCacheProvenance(output, status, 0)
		if payload, marshalErr := json.Marshal(output); marshalErr == nil {
			options.Cache.PutAsync(cache.Entry{
				Key:            key,
				Classification: classification,
				Payload:        payload,
			})
		}
		return output, nil
	}
}

func detailAllowedForRequest(name string, arguments map[string]any) bool {
	if name != "stratz_query_matches" {
		return true
	}
	mode, _ := arguments["mode"].(string)
	detail, present := arguments["detail_level"]
	if !present {
		return true
	}
	text, ok := detail.(string)
	if !ok {
		if typed, typedOK := detail.(contracts.DetailLevel); typedOK {
			text, ok = string(typed), true
		}
	}
	if !ok {
		return false
	}
	switch mode {
	case "exact":
		return text == "summary" || text == "players" || text == "standard" || text == "full"
	case "player_history":
		return text == "summary" || text == "players"
	case "league_history":
		return text == "summary"
	case "live":
		return false
	default:
		return false
	}
}

func cacheDetailLevel(name string, arguments map[string]any) string {
	if name == "stratz_query_matches" {
		return string(matchDetailInput(arguments))
	}
	return ""
}

func staleFallbackAllowed(err error) bool {
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) {
		return false
	}
	switch executionErr.Code {
	case contracts.ErrorCodeUpstreamNetworkError,
		contracts.ErrorCodeUpstreamTLSError,
		contracts.ErrorCodeUpstreamTimeout,
		contracts.ErrorCodeRateLimited,
		contracts.ErrorCodeUpstreamWAFBlocked,
		contracts.ErrorCodeUpstreamError:
		return true
	default:
		return false
	}
}

func cacheArguments(arguments map[string]any) map[string]any {
	result := make(map[string]any, len(arguments))
	for key, value := range arguments {
		if key == "fresh" {
			continue
		}
		result[key] = value
	}
	return result
}

func decodeCachedOutput(payload []byte) (map[string]any, bool) {
	var output map[string]any
	if json.Unmarshal(payload, &output) != nil {
		return nil, false
	}
	return output, true
}

func setCacheProvenance(output any, status cache.LookupStatus, age time.Duration) {
	envelope, ok := output.(map[string]any)
	if !ok {
		return
	}
	provenance, ok := envelope["provenance"].(map[string]any)
	if !ok {
		return
	}
	cacheInfo, ok := provenance["cache"].(map[string]any)
	if !ok {
		cacheInfo = map[string]any{}
		provenance["cache"] = cacheInfo
	}
	cacheInfo["status"] = string(status)
	if status == cache.LookupHit || status == cache.LookupStale {
		cacheInfo["age_seconds"] = int64(age / time.Second)
	} else {
		cacheInfo["age_seconds"] = nil
	}
}

func appendWarning(output map[string]any, warning string) {
	if strings, ok := output["warnings"].([]string); ok {
		output["warnings"] = append(strings, warning)
		return
	}
	values, _ := output["warnings"].([]any)
	output["warnings"] = append(values, warning)
}
