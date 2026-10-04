package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func TestQueryCacheClassificationUsesValidatedModeAndOptions(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input map[string]any
		want  cache.Class
	}{
		{"heroes reference", "stratz_query_heroes", map[string]any{"mode": "exact", "heroes": []any{1}}, cache.ClassPublicReference},
		{"heroes statistics", "stratz_query_heroes", map[string]any{"mode": "search", "query": "axe", "include_statistics": true}, cache.ClassPublicRecent},
		{"players", "stratz_query_players", map[string]any{"mode": "exact", "player_ids": []any{"1"}}, cache.ClassProfileSensitive},
		{"leagues exact", "stratz_query_leagues", map[string]any{"mode": "exact", "league_ids": []any{"1"}}, cache.ClassPublicReference},
		{"leagues search", "stratz_query_leagues", map[string]any{"mode": "search"}, cache.ClassPublicRecent},
		{"matches exact", "stratz_query_matches", map[string]any{"mode": "exact", "match_ids": []any{"1"}}, cache.ClassPublicRecent},
		{"matches player", "stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1"}, cache.ClassProfileSensitive},
		{"matches league", "stratz_query_matches", map[string]any{"mode": "league_history", "league_id": "1"}, cache.ClassPublicRecent},
		{"matches live", "stratz_query_matches", map[string]any{"mode": "live"}, cache.ClassPublicLive},
		{"constants", "stratz_query_constants", map[string]any{"mode": "types", "types": []any{"heroes"}}, cache.ClassPublicReference},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			specification, ok := cacheSpecificationFor(test.tool, test.input)
			if !ok {
				t.Fatal("request was not classified")
			}
			if specification.class != test.want {
				t.Fatalf("class = %q, want %q", specification.class, test.want)
			}
		})
	}
	if _, ok := cacheSpecificationFor("stratz_query_matches", map[string]any{"mode": "future"}); ok {
		t.Fatal("unknown mode was cacheable")
	}
}

func TestCachedQueryHandlerHonorsHitFreshAndRawBypass(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store, err := cache.Open(cache.Options{Config: cfg.Cache, Features: cfg.Features, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	options := Options{SchemaVersion: "schema-v2", Config: cfg, Cache: store, CacheNamespace: "fixture", Now: func() time.Time { return now }}
	calls := 0
	handler := cachedToolHandler(options, "stratz_query_players", func(context.Context, any) (any, error) {
		calls++
		return curatedEnvelope(options, "query_players", "", map[string]any{"mode": "exact", "items": []any{}}, nil, false, nil, nil), nil
	})
	input := map[string]any{"mode": "exact", "player_ids": []any{"1"}}
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, store, 1)
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 after hit", calls)
	}
	output, err := handler(context.Background(), map[string]any{"mode": "exact", "player_ids": []any{"1"}, "fresh": true})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 after fresh", calls)
	}
	assertCacheStatus(t, output, "bypass")

	output, err = handler(context.Background(), map[string]any{"mode": "exact", "player_ids": []any{"1"}, "include_raw": true})
	if err != nil {
		t.Fatal(err)
	}
	assertCacheStatus(t, output, "bypass")
}

func TestCachedQueryHandlerUsesStaleOnlyForTransientFailures(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store, err := cache.Open(cache.Options{Config: cfg.Cache, Features: cfg.Features, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	options := Options{SchemaVersion: "schema-v2", Config: cfg, Cache: store, CacheNamespace: "fixture", Now: func() time.Time { return now }}
	var handlerErr error
	handler := cachedToolHandler(options, "stratz_query_matches", func(context.Context, any) (any, error) {
		if handlerErr != nil {
			return nil, handlerErr
		}
		return curatedEnvelope(options, "query_matches", contracts.DetailLevelStandard, map[string]any{"mode": "exact", "items": []any{}}, nil, false, nil, nil), nil
	})
	input := map[string]any{"mode": "exact", "match_ids": []any{"1"}}
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, store, 1)
	now = now.Add(cfg.Cache.PublicRecentTTL + time.Second)
	handlerErr = &ExecutionError{Code: contracts.ErrorCodeUpstreamNetworkError, Message: "network", Details: map[string]any{}}
	output, err := handler(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertCacheStatus(t, output, "stale")

	handlerErr = &ExecutionError{Code: contracts.ErrorCodeInvalidArgument, Message: "invalid", Details: map[string]any{}}
	if output, err := handler(context.Background(), input); !errors.Is(err, handlerErr) || output != nil {
		t.Fatalf("invalid request fallback = %#v, %v", output, err)
	}
}

func waitForCacheEntries(t *testing.T, store *cache.Store, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		stats, err := store.Stats(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if stats.Entries == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache entries = %d, want %d", stats.Entries, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func assertCacheStatus(t *testing.T, output any, want string) {
	t.Helper()
	envelope := output.(map[string]any)
	provenance := envelope["provenance"].(map[string]any)
	cacheInfo := provenance["cache"].(map[string]any)
	if cacheInfo["status"] != want {
		t.Fatalf("cache status = %#v, want %q", cacheInfo["status"], want)
	}
}
