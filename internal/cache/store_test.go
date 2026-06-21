package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
)

func TestLookupHitMissStaleAndFreshBypass(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {})
	defer store.Close()

	key, classification := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	result, err := store.Lookup(context.Background(), LookupRequest{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupMiss {
		t.Fatalf("initial lookup status = %q, want miss", result.Status)
	}
	if err := store.Put(context.Background(), Entry{
		Key:            key,
		Classification: classification,
		Payload:        []byte(`{"hero_id":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	result, err = store.Lookup(context.Background(), LookupRequest{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupHit || string(result.Payload) != `{"hero_id":1}` {
		t.Fatalf("lookup result = %+v", result)
	}

	now = now.Add(classification.TTL + time.Second)
	result, err = store.Lookup(context.Background(), LookupRequest{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupMiss {
		t.Fatalf("expired lookup status = %q, want miss", result.Status)
	}

	result, err = store.Lookup(context.Background(), LookupRequest{
		Key:        key,
		AllowStale: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupStale || string(result.Payload) != `{"hero_id":1}` {
		t.Fatalf("stale lookup result = %+v", result)
	}

	result, err = store.Lookup(context.Background(), LookupRequest{
		Key:   key,
		Fresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupBypass {
		t.Fatalf("fresh lookup status = %q, want bypass", result.Status)
	}
}

func TestClassificationExclusionsAndNamespaceIsolation(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	excluded := ResolveClassification(
		cfg.Cache,
		cfg.Features,
		"players",
		ClassPublicRecent,
		true,
	)
	if excluded.Cacheable {
		t.Fatal("include_raw classification unexpectedly cacheable")
	}
	rawExcluded := ResolveClassification(
		cfg.Cache,
		cfg.Features,
		"raw",
		ClassRawUnclassified,
		false,
	)
	if rawExcluded.Cacheable {
		t.Fatal("raw classification unexpectedly cacheable")
	}

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {})
	defer store.Close()

	keyA, classification := testKey(t, store, "token-a", "players", ClassProfileSensitive)
	keyB, _ := testKey(t, store, "token-b", "players", ClassProfileSensitive)
	if err := store.Put(context.Background(), Entry{
		Key:            keyA,
		Classification: classification,
		Payload:        []byte(`{"player":"a"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), Entry{
		Key:            keyB,
		Classification: classification,
		Payload:        []byte(`{"player":"b"}`),
	}); err != nil {
		t.Fatal(err)
	}

	resultA, err := store.Lookup(context.Background(), LookupRequest{Key: keyA})
	if err != nil {
		t.Fatal(err)
	}
	resultB, err := store.Lookup(context.Background(), LookupRequest{Key: keyB})
	if err != nil {
		t.Fatal(err)
	}
	if string(resultA.Payload) != `{"player":"a"}` || string(resultB.Payload) != `{"player":"b"}` {
		t.Fatalf("namespace isolation failed: A=%q B=%q", resultA.Payload, resultB.Payload)
	}
}

func TestCompressionThresholdAndStats(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {})
	defer store.Close()

	keySmall, classification := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	keyLarge, _ := testKey(t, store, "token-a", "matches", ClassPublicHistorical)
	smallPayload := []byte(`{"x":1}`)
	largePayload := []byte(strings.Repeat("x", compressionThreshold+512))

	if err := store.Put(context.Background(), Entry{
		Key:            keySmall,
		Classification: classification,
		Payload:        smallPayload,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), Entry{
		Key:            keyLarge,
		Classification: ResolveClassification(store.cacheConfig, store.features, "matches", ClassPublicHistorical, false),
		Payload:        largePayload,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := store.db.QueryContext(
		context.Background(),
		`SELECT domain, compression, logical_size_bytes, stored_size_bytes FROM cache_entries ORDER BY domain`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]struct {
		Compression string
		Logical     int
		Stored      int
	}{}
	for rows.Next() {
		var domain, compression string
		var logical, stored int
		if err := rows.Scan(&domain, &compression, &logical, &stored); err != nil {
			t.Fatal(err)
		}
		got[domain] = struct {
			Compression string
			Logical     int
			Stored      int
		}{compression, logical, stored}
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if got["heroes"].Compression != "none" {
		t.Fatalf("heroes compression = %q, want none", got["heroes"].Compression)
	}
	if got["matches"].Compression != "zstd" {
		t.Fatalf("matches compression = %q, want zstd", got["matches"].Compression)
	}
	if got["matches"].Stored >= got["matches"].Logical {
		t.Fatalf("compressed payload not smaller: %+v", got["matches"])
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Status != StatusHealthy || stats.Entries != 2 || stats.Namespaces != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(stats.Domains) != 2 {
		t.Fatalf("domain stats = %+v", stats.Domains)
	}
}

func TestEvictionUsesLeastRecentlyUsedEntries(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {
		cfg.Cache.MaxSizeBytes = 80
	})
	defer store.Close()

	key1, class1 := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	now = now.Add(time.Millisecond)
	key2, class2 := testKey(t, store, "token-a", "matches", ClassPublicHistorical)
	now = now.Add(time.Millisecond)
	key3, class3 := testKey(t, store, "token-a", "players", ClassProfileSensitive)

	for _, item := range []struct {
		key   Key
		class Classification
		body  string
	}{
		{key1, class1, strings.Repeat("a", 40)},
		{key2, class2, strings.Repeat("b", 40)},
	} {
		if err := store.Put(context.Background(), Entry{
			Key:            item.key,
			Classification: item.class,
			Payload:        []byte(item.body),
		}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Millisecond)
	}
	if _, err := store.Lookup(context.Background(), LookupRequest{Key: key1}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Millisecond)
	if err := store.Put(context.Background(), Entry{
		Key:            key3,
		Classification: class3,
		Payload:        []byte(strings.Repeat("c", 40)),
	}); err != nil {
		t.Fatal(err)
	}

	result1, err := store.Lookup(context.Background(), LookupRequest{Key: key1})
	if err != nil {
		t.Fatal(err)
	}
	result2, err := store.Lookup(context.Background(), LookupRequest{Key: key2})
	if err != nil {
		t.Fatal(err)
	}
	result3, err := store.Lookup(context.Background(), LookupRequest{Key: key3})
	if err != nil {
		t.Fatal(err)
	}
	if result1.Status != LookupHit || result3.Status != LookupHit || result2.Status != LookupMiss {
		t.Fatalf("eviction results: 1=%q 2=%q 3=%q", result1.Status, result2.Status, result3.Status)
	}
}

func TestConcurrentStoresShareSQLite(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	cfg := config.Defaults(directory)
	storeA, err := Open(Options{
		Config:   cfg.Cache,
		Features: cfg.Features,
		Now:      nowPointer(&now),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := Open(Options{
		Config:   cfg.Cache,
		Features: cfg.Features,
		Now:      nowPointer(&now),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()

	var wg sync.WaitGroup
	type prepared struct {
		key   Key
		class Classification
	}
	a := make([]prepared, 10)
	b := make([]prepared, 10)
	for index := 0; index < 10; index++ {
		a[index].key, a[index].class = testKeyFromConfig(t, cfg, "token-a", "heroes", ClassPublicReference, map[string]any{"id": index})
		b[index].key, b[index].class = testKeyFromConfig(t, cfg, "token-b", "matches", ClassPublicHistorical, map[string]any{"id": index})
	}
	for index := 0; index < 10; index++ {
		index := index
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := storeA.Put(context.Background(), Entry{
				Key:            a[index].key,
				Classification: a[index].class,
				Payload:        []byte(strings.Repeat("a", 32)),
			}); err != nil {
				t.Errorf("storeA put: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := storeB.Put(context.Background(), Entry{
				Key:            b[index].key,
				Classification: b[index].class,
				Payload:        []byte(strings.Repeat("b", 32)),
			}); err != nil {
				t.Errorf("storeB put: %v", err)
			}
		}()
	}
	wg.Wait()

	stats, err := storeA.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 20 {
		t.Fatalf("entries = %d, want 20", stats.Entries)
	}
}

func TestPutAsyncDoesNotRegisterAfterCloseStarts(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {})
	key, classification := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	entry := Entry{
		Key:            key,
		Classification: classification,
		Payload:        []byte(`{"hero_id":1}`),
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < 32; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for attempt := 0; attempt < 32; attempt++ {
				store.PutAsync(entry)
			}
		}()
	}
	close(start)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	store.PutAsync(entry)
}

func TestOpenRejectsSymlinkAndAppliesPermissions(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Defaults(directory)
	target := filepath.Join(directory, "target.db")
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.Cache.Directory, cacheFileName)
	if err := os.MkdirAll(cfg.Cache.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Open(Options{Config: cfg.Cache, Features: cfg.Features}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {})
	defer store.Close()
	key, class := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	if err := store.Put(context.Background(), Entry{
		Key:            key,
		Classification: class,
		Payload:        []byte(`{"x":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		assertPerm(t, filepath.Dir(store.path), 0o700)
		assertPerm(t, store.path, 0o600)
		for _, suffix := range []string{"-wal", "-shm"} {
			candidate := store.path + suffix
			if _, err := os.Stat(candidate); err == nil {
				assertPerm(t, candidate, 0o600)
			}
		}
	}
}

func TestClearVariantsAndStats(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	store := openTestStore(t, nowPointer(&now), func(cfg *config.Config) {})
	defer store.Close()

	keyHeroA, classHero := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	keyHeroB, _ := testKey(t, store, "token-b", "heroes", ClassPublicReference)
	keyPlayerA, classPlayer := testKey(t, store, "token-a", "players", ClassProfileSensitive)
	for _, entry := range []Entry{
		{Key: keyHeroA, Classification: classHero, Payload: []byte(`{"hero":"a"}`)},
		{Key: keyHeroB, Classification: classHero, Payload: []byte(`{"hero":"b"}`)},
		{Key: keyPlayerA, Classification: classPlayer, Payload: []byte(`{"player":"a"}`)},
	} {
		if err := store.Put(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
	}

	clearDomain, err := store.Clear(context.Background(), ClearOptions{Domain: "heroes"})
	if err != nil {
		t.Fatal(err)
	}
	if clearDomain.Deleted != 2 {
		t.Fatalf("domain clear deleted = %d, want 2", clearDomain.Deleted)
	}
	result, err := store.Lookup(context.Background(), LookupRequest{Key: keyPlayerA})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupHit {
		t.Fatalf("player entry status = %q, want hit", result.Status)
	}

	if err := store.Put(context.Background(), Entry{
		Key:            keyHeroA,
		Classification: classHero,
		Payload:        []byte(`{"hero":"a"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), Entry{
		Key:            keyHeroB,
		Classification: classHero,
		Payload:        []byte(`{"hero":"b"}`),
	}); err != nil {
		t.Fatal(err)
	}

	clearToken, err := store.Clear(context.Background(), ClearOptions{Namespace: keyHeroA.Namespace})
	if err != nil {
		t.Fatal(err)
	}
	if clearToken.Deleted != 2 {
		t.Fatalf("namespace clear deleted = %d, want 2", clearToken.Deleted)
	}
	resultA, err := store.Lookup(context.Background(), LookupRequest{Key: keyHeroA})
	if err != nil {
		t.Fatal(err)
	}
	resultB, err := store.Lookup(context.Background(), LookupRequest{Key: keyHeroB})
	if err != nil {
		t.Fatal(err)
	}
	if resultA.Status != LookupMiss || resultB.Status != LookupHit {
		t.Fatalf("namespace clear results: A=%q B=%q", resultA.Status, resultB.Status)
	}

	clearAll, err := store.Clear(context.Background(), ClearOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if clearAll.Deleted == 0 {
		t.Fatal("clear all deleted nothing")
	}
	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 0 {
		t.Fatalf("entries after clear = %d, want 0", stats.Entries)
	}
}

func TestCacheDisablesItselfAfterLockedWriteFailure(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults(t.TempDir())
	store, err := Open(Options{
		Config:      cfg.Cache,
		Features:    cfg.Features,
		Now:         nowPointer(&now),
		BusyTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	lockDB, err := sql.Open("sqlite", store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer lockDB.Close()
	lockDB.SetMaxOpenConns(1)
	connection, err := lockDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")

	key, class := testKey(t, store, "token-a", "heroes", ClassPublicReference)
	err = store.Put(context.Background(), Entry{
		Key:            key,
		Classification: class,
		Payload:        []byte(`{"x":1}`),
	})
	if err == nil {
		t.Fatal("expected locked write failure")
	}
	if store.Status() != StatusDegraded {
		t.Fatalf("store status = %q, want degraded", store.Status())
	}
	result, err := store.Lookup(context.Background(), LookupRequest{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != LookupDisabled {
		t.Fatalf("lookup status after degradation = %q, want disabled", result.Status)
	}
}

func TestOpenFailsOnCorruptDatabase(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Defaults(directory)
	if err := os.MkdirAll(cfg.Cache.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Cache.Directory, cacheFileName), []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Config: cfg.Cache, Features: cfg.Features}); err == nil {
		t.Fatal("expected corrupt database error")
	}
}

func openTestStore(
	t *testing.T,
	now func() time.Time,
	mutate func(*config.Config),
) *Store {
	t.Helper()
	cfg := config.Defaults(t.TempDir())
	if mutate != nil {
		mutate(&cfg)
	}
	store, err := Open(Options{
		Config:   cfg.Cache,
		Features: cfg.Features,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testKey(t *testing.T, store *Store, token, domain string, class Class) (Key, Classification) {
	t.Helper()
	cfg := config.Config{Cache: store.cacheConfig, Features: store.features}
	return testKeyFromConfig(t, cfg, token, domain, class, map[string]any{"id": 1})
}

func testKeyFromConfig(
	t *testing.T,
	cfg config.Config,
	token, domain string,
	class Class,
	arguments any,
) (Key, Classification) {
	t.Helper()
	classification := ResolveClassification(cfg.Cache, cfg.Features, domain, class, false)
	if !classification.Cacheable {
		t.Fatalf("classification not cacheable: %+v", classification)
	}
	key, err := CanonicalKey(KeyInput{
		Namespace:     NamespaceForToken(token),
		Domain:        domain,
		Class:         class,
		Operation:     domain + ".get",
		Arguments:     arguments,
		SchemaVersion: "sha256:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return key, classification
}

func nowPointer(now *time.Time) func() time.Time {
	return func() time.Time {
		return *now
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func TestCanonicalKeyIsDeterministic(t *testing.T) {
	keyA, err := CanonicalKey(KeyInput{
		Namespace:     NamespaceForToken("token"),
		Domain:        "heroes",
		Class:         ClassPublicReference,
		Operation:     "heroes.get",
		Arguments:     map[string]any{"b": 2, "a": 1},
		SchemaVersion: "sha256:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := CanonicalKey(KeyInput{
		Namespace:     NamespaceForToken("token"),
		Domain:        "heroes",
		Class:         ClassPublicReference,
		Operation:     "heroes.get",
		Arguments:     map[string]any{"a": 1, "b": 2},
		SchemaVersion: "sha256:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if keyA != keyB {
		t.Fatalf("keys differ: %+v vs %+v", keyA, keyB)
	}
	document, err := canonicalDocument(KeyInput{
		Namespace:     keyA.Namespace,
		Domain:        keyA.Domain,
		Class:         keyA.Class,
		Operation:     "heroes.get",
		Arguments:     map[string]any{"a": 1},
		SchemaVersion: "sha256:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["format_version"] != float64(FormatVersion) {
		t.Fatalf("format_version = %v", decoded["format_version"])
	}
}
