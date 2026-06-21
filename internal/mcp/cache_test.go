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

func TestCachedToolHandlerHonorsHitAndFresh(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store, err := cache.Open(cache.Options{
		Config: cfg.Cache, Features: cfg.Features, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	options := Options{
		SchemaVersion:  "schema-v1",
		Config:         cfg,
		Cache:          store,
		CacheNamespace: cache.NamespaceForToken("token"),
		Now:            func() time.Time { return now },
	}
	calls := 0
	handler := cachedToolHandler(
		options,
		"stratz_get_player",
		cacheSpecifications["stratz_get_player"],
		func(context.Context, any) (any, error) {
			calls++
			return curatedEnvelope(
				options,
				"get_player",
				contracts.DetailLevelStandard,
				contracts.Player{AccountID: "1", IsPrivate: false},
				nil,
				false,
				nil,
				nil,
			), nil
		},
	)
	input := map[string]any{"player_id": "1"}
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		stats, statsErr := store.Stats(context.Background())
		if statsErr != nil {
			t.Fatal(statsErr)
		}
		if stats.Entries == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("asynchronous cache write did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	output, err := handler(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	assertCacheStatus(t, output, "hit")

	output, err = handler(context.Background(), map[string]any{"player_id": "1", "fresh": true})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("handler calls after fresh = %d, want 2", calls)
	}
	assertCacheStatus(t, output, "bypass")
}

func TestCachedToolHandlerOnlyUsesStaleForTransientUpstreamErrors(t *testing.T) {
	tests := []struct {
		name      string
		code      contracts.ErrorCode
		wantStale bool
	}{
		{name: "network", code: contracts.ErrorCodeUpstreamNetworkError, wantStale: true},
		{name: "timeout", code: contracts.ErrorCodeUpstreamTimeout, wantStale: true},
		{name: "rate limit", code: contracts.ErrorCodeRateLimited, wantStale: true},
		{name: "invalid argument", code: contracts.ErrorCodeInvalidArgument},
		{name: "expired cursor", code: contracts.ErrorCodeCursorExpired},
		{name: "authentication", code: contracts.ErrorCodeAuthenticationFailed},
		{name: "private", code: contracts.ErrorCodePrivate},
		{name: "data not ready", code: contracts.ErrorCodeDataNotReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Defaults(t.TempDir())
			now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
			store, err := cache.Open(cache.Options{
				Config: cfg.Cache, Features: cfg.Features, Now: func() time.Time { return now },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			options := Options{
				SchemaVersion:  "schema-v1",
				Config:         cfg,
				Cache:          store,
				CacheNamespace: cache.NamespaceForToken("token"),
				Now:            func() time.Time { return now },
			}
			var handlerErr error
			handler := cachedToolHandler(
				options,
				"stratz_get_player",
				cacheSpecifications["stratz_get_player"],
				func(context.Context, any) (any, error) {
					if handlerErr != nil {
						return nil, handlerErr
					}
					return curatedEnvelope(
						options,
						"get_player",
						contracts.DetailLevelStandard,
						contracts.Player{AccountID: "1", IsPrivate: false},
						nil,
						false,
						nil,
						nil,
					), nil
				},
			)
			input := map[string]any{"player_id": "1"}
			if _, err := handler(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			waitForCacheEntries(t, store, 1)
			now = now.Add(cfg.Cache.ProfileSensitiveTTL + time.Second)
			handlerErr = &ExecutionError{
				Code: test.code, Message: "fixture failure", Details: map[string]any{},
			}
			output, err := handler(context.Background(), input)
			if test.wantStale {
				if err != nil {
					t.Fatalf("transient error did not use stale cache: %v", err)
				}
				assertCacheStatus(t, output, "stale")
				return
			}
			if !errors.Is(err, handlerErr) {
				t.Fatalf("semantic error = %v, want original %v", err, handlerErr)
			}
			if output != nil {
				t.Fatalf("semantic error returned cached output: %#v", output)
			}
		})
	}
}

func TestEvolvingMatchToolsUseRecentCacheClass(t *testing.T) {
	for _, tool := range []string{
		"stratz_get_match",
		"stratz_batch_get_matches",
		"stratz_list_league_matches",
	} {
		if got := cacheSpecifications[tool].class; got != cache.ClassPublicRecent {
			t.Fatalf("%s cache class = %q, want %q", tool, got, cache.ClassPublicRecent)
		}
	}
}

func TestPlayerHistoryUsesProfileSensitiveTTL(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults(t.TempDir())
	store, err := cache.Open(cache.Options{
		Config: cfg.Cache, Features: cfg.Features, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	options := Options{
		Config: cfg, Cache: store, CacheNamespace: "fixture",
		SchemaVersion: "schema", Now: func() time.Time { return now },
	}
	calls := 0
	handler := cachedToolHandler(
		options,
		"stratz_list_player_matches",
		cacheSpecifications["stratz_list_player_matches"],
		func(context.Context, any) (any, error) {
			calls++
			return curatedEnvelope(
				options,
				"list_player_matches",
				contracts.DetailLevelSummary,
				map[string]any{"items": []any{}},
				nil,
				false,
				nil,
				nil,
			), nil
		},
	)
	input := map[string]any{"player_id": "1"}
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, store, 1)
	now = now.Add(cfg.Cache.PublicRecentTTL + time.Second)
	output, err := handler(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want cached hit", calls)
	}
	assertCacheStatus(t, output, "hit")
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
