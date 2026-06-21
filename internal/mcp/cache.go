package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/cache"
)

type cacheSpecification struct {
	domain string
	class  cache.Class
}

var cacheSpecifications = map[string]cacheSpecification{
	"stratz_get_player":          {domain: "players", class: cache.ClassProfileSensitive},
	"stratz_list_player_matches": {domain: "matches", class: cache.ClassPublicRecent},
	"stratz_batch_get_players":   {domain: "players", class: cache.ClassProfileSensitive},
	"stratz_get_match":           {domain: "matches", class: cache.ClassPublicHistorical},
	"stratz_batch_get_matches":   {domain: "matches", class: cache.ClassPublicHistorical},
	"stratz_get_hero":            {domain: "heroes", class: cache.ClassPublicReference},
	"stratz_batch_get_heroes":    {domain: "heroes", class: cache.ClassPublicReference},
	"stratz_get_hero_stats":      {domain: "heroes", class: cache.ClassPublicRecent},
	"stratz_get_constants":       {domain: "constants", class: cache.ClassPublicReference},
	"stratz_get_league":          {domain: "leagues", class: cache.ClassPublicReference},
	"stratz_list_leagues":        {domain: "leagues", class: cache.ClassPublicRecent},
	"stratz_list_league_matches": {domain: "matches", class: cache.ClassPublicHistorical},
	"stratz_list_live_matches":   {domain: "live", class: cache.ClassPublicLive},
}

func cachedToolHandler(
	options Options,
	name string,
	specification cacheSpecification,
	handler ToolHandler,
) ToolHandler {
	if handler == nil || options.Cache == nil {
		return handler
	}
	return func(ctx context.Context, input any) (any, error) {
		arguments, _ := input.(map[string]any)
		fresh, _ := arguments["fresh"].(bool)
		includeRaw, _ := arguments["include_raw"].(bool)
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
			DetailLevel:   string(detailInput(arguments)),
			IncludeRaw:    includeRaw,
			SchemaVersion: options.SchemaVersion,
		})
		if keyErr != nil || !classification.Cacheable {
			output, err := handler(ctx, input)
			if err == nil {
				status := cache.LookupDisabled
				if includeRaw {
					status = cache.LookupBypass
				}
				setCacheProvenance(output, status, 0)
			}
			return output, err
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

		output, err := handler(ctx, input)
		if err != nil && !fresh {
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
			return nil, err
		}
		if err != nil {
			return nil, err
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
