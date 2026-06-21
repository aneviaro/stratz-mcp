package mcp

import (
	"context"
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

func assertCacheStatus(t *testing.T, output any, want string) {
	t.Helper()
	envelope := output.(map[string]any)
	provenance := envelope["provenance"].(map[string]any)
	cacheInfo := provenance["cache"].(map[string]any)
	if cacheInfo["status"] != want {
		t.Fatalf("cache status = %#v, want %q", cacheInfo["status"], want)
	}
}
