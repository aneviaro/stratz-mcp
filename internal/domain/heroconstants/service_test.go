package heroconstants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

type fixtureExecutor struct {
	execute func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error)
	calls   int
}

func (executor *fixtureExecutor) Execute(
	_ context.Context,
	budget *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	executor.calls++
	if !budget.Take() {
		return nil, &stratz.Error{
			Code: contracts.ErrorCodeRequestBudgetExceeded, Message: "budget exhausted", Details: map[string]any{},
		}
	}
	return executor.execute(budget, request)
}

func TestHeroResolutionByIDNameSlugAmbiguityAndBatchDuplicates(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetConstants" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		return response(constantsFixture), nil
	}}
	service := mustService(t, executor)
	for _, test := range []struct {
		input any
		id    int64
	}{
		{json.Number("1"), 1},
		{"Axe", 1},
		{"axe", 1},
		{"queen-of-pain", 2},
	} {
		result, err := service.GetHero(context.Background(), test.input)
		if err != nil {
			t.Fatalf("GetHero(%v): %T %#v", test.input, err, err)
		}
		if result.Data.HeroID != test.id {
			t.Fatalf("GetHero(%v) id = %d, want %d", test.input, result.Data.HeroID, test.id)
		}
	}

	_, err := service.GetHero(context.Background(), "Twin")
	var domainErr *Error
	if !errors.As(err, &domainErr) ||
		domainErr.Code != contracts.ErrorCodeInvalidArgument ||
		len(domainErr.Details["suggestions"].([]map[string]any)) != 2 {
		t.Fatalf("ambiguity error = %#v", err)
	}

	executor.calls = 0
	result, err := service.BatchHeroes(context.Background(), []any{"axe", json.Number("2"), "Axe"})
	if err != nil {
		t.Fatalf("%T %#v", err, err)
	}
	if executor.calls != 1 {
		t.Fatalf("calls = %d, want 1", executor.calls)
	}
	got := []int64{result.Data[0].HeroID, result.Data[1].HeroID, result.Data[2].HeroID}
	if want := []int64{1, 2, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("batch = %v, want %v", got, want)
	}
}

func TestHeroBatchHonorsConfiguredMaximum(t *testing.T) {
	service, err := New(Options{
		Executor:            &fixtureExecutor{},
		MaxUpstreamRequests: 5,
		MaxBatchSize:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BatchHeroes(context.Background(), []any{1, 2, 3}); err == nil {
		t.Fatal("batch accepted more than the configured maximum")
	}
}

func TestConstantsTypesExplicitAllAndMissingRanks(t *testing.T) {
	executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(constantsFixture), nil
	}}
	service := mustService(t, executor)
	expected := map[string]int{
		"heroes": 4, "items": 1, "abilities": 1, "game_modes": 1, "regions": 1, "ranks": 0, "all": 8,
	}
	for kind, count := range expected {
		result, err := service.GetConstants(context.Background(), kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if result.Data.Type != kind || len(result.Data.Items) != count {
			t.Fatalf("%s result = %#v", kind, result.Data)
		}
		if (kind == "ranks" || kind == "all") && len(result.Warnings) != 1 {
			t.Fatalf("%s warnings = %#v", kind, result.Warnings)
		}
	}
}

func TestHeroStatisticsBucketsApplyDateRankAndRoleFilters(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetHeroStatsWeek" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		variables := request.Variables.(map[string]any)
		if !reflect.DeepEqual(variables["heroIds"], []int64{1}) ||
			!reflect.DeepEqual(variables["bracketIds"], []string{"IMMORTAL"}) ||
			!reflect.DeepEqual(variables["positionIds"], []string{"POSITION_1", "POSITION_2", "POSITION_3"}) {
			t.Fatalf("variables = %#v", variables)
		}
		return response(fmt.Sprintf(`{"heroStats":{"stats":[
			{"heroId":0,"period":%d,"matchCount":20,"winCount":12},
			{"heroId":1,"period":%d,"matchCount":20,"winCount":13},
			{"heroId":1,"period":%d,"matchCount":999,"winCount":999}
		]}}`,
			time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC).Unix(),
			time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC).Unix(),
			time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC).Unix(),
		)), nil
	}}
	service := mustServiceAt(t, executor, now)
	from := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	rank := "immortal"
	role := "core"
	result, err := service.GetHeroStats(context.Background(), StatsFilters{
		Hero: json.Number("1"), From: &from, To: &to, RankBracket: &rank, Role: &role,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EffectiveRange == nil ||
		result.Data.WinRate == nil || math.Abs(*result.Data.WinRate-0.625) > 0.000001 ||
		result.Data.SampleSize != 40 || result.Data.PickRate != nil || result.Data.BanRate != nil ||
		len(result.Warnings) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestHeroStatisticsRejectUnsupportedDimensions(t *testing.T) {
	service := mustServiceAt(t, &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		t.Fatal("upstream should not be called")
		return nil, nil
	}}, time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC))
	lane := "mid"
	_, err := service.GetHeroStats(context.Background(), StatsFilters{Hero: 1, Lane: &lane})
	assertCode(t, err, contracts.ErrorCodeInvalidArgument)
	_, err = service.GetHeroStats(context.Background(), StatsFilters{Hero: 1, IncludeMatchups: true})
	assertCode(t, err, contracts.ErrorCodeInvalidArgument)
	patch := "7.39"
	_, err = service.GetHeroStats(context.Background(), StatsFilters{Hero: 1, PatchID: &patch})
	assertCode(t, err, contracts.ErrorCodeInvalidArgument)
	if err == nil {
		t.Fatal("expected unsupported filter error")
	}
}

func TestPatchStatisticsRejectIncompatibleBucket(t *testing.T) {
	service := mustServiceAt(t, &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		t.Fatal("upstream should not be called")
		return nil, nil
	}}, time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC))
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	patch := "7.39"
	_, err := service.GetHeroStats(context.Background(), StatsFilters{
		Hero: json.Number("1"), From: &from, To: &to, PatchID: &patch,
	})
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != contracts.ErrorCodeInvalidArgument {
		t.Fatalf("error = %#v", err)
	}
}

func TestStatisticsBucketBoundariesAndOperations(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		days      int
		bucket    string
		operation string
	}{
		{days: 7, bucket: "day", operation: "StratzGetHeroStatsDay"},
		{days: 60, bucket: "week", operation: "StratzGetHeroStatsWeek"},
		{days: 240, bucket: "month", operation: "StratzGetHeroStatsMonth"},
	} {
		from := now.AddDate(0, 0, -test.days)
		bucket, effective, err := translateRange(now, &from, &now)
		if err != nil {
			t.Fatalf("%d days: %v", test.days, err)
		}
		_, operation := statisticsOperation(bucket)
		if bucket != test.bucket || operation != test.operation || effective.From.After(from) || effective.To.Before(now) {
			t.Fatalf("%d days: bucket=%q operation=%q range=%#v", test.days, bucket, operation, effective)
		}
	}
}

func TestStatisticsRejectsRoundedRangeBeyondMaximum(t *testing.T) {
	end := time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -365)
	_, _, err := translateRange(end, &start, &end)
	if err == nil || err.Code != contracts.ErrorCodeInvalidArgument {
		t.Fatalf("error = %#v", err)
	}
}

func TestCleanTruncatesAtRuneBoundary(t *testing.T) {
	if got := clean("żółw", 3); got != "żół" {
		t.Fatalf("cleaned value = %q", got)
	}
}

func TestHeroServiceMapsUpstreamAndProtocolFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		response *stratz.Response
		err      error
		want     contracts.ErrorCode
	}{
		{
			name: "upstream",
			err: &stratz.Error{
				Code: contracts.ErrorCodeAuthenticationFailed, Message: "auth",
				Details: map[string]any{},
			},
			want: contracts.ErrorCodeAuthenticationFailed,
		},
		{name: "nil response", want: contracts.ErrorCodeUpstreamProtocolError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := mustService(t, &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
				return test.response, test.err
			}})
			_, err := service.GetHero(context.Background(), json.Number("1"))
			var domainErr *Error
			if !errors.As(err, &domainErr) || domainErr.Code != test.want {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestConstantsInMemoryCacheDeduplicatesAndExpires(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(constantsFixture), nil
	}}
	service, err := New(Options{
		Executor:            executor,
		MaxUpstreamRequests: 5,
		ConstantsTTL:        time.Hour,
		Now:                 func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetConstants(context.Background(), "heroes"); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("after first call: calls = %d, want 1", executor.calls)
	}
	// A repeat within the TTL is served from the in-memory cache.
	if _, err := service.GetConstants(context.Background(), "heroes"); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("after cached call: calls = %d, want 1", executor.calls)
	}
	// A different constants consumer shares the same cached snapshot.
	if _, err := service.GetHero(context.Background(), json.Number("1")); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("after cross-consumer cached call: calls = %d, want 1", executor.calls)
	}
	// Advancing the clock past the TTL forces one fresh fetch.
	now = now.Add(2 * time.Hour)
	if _, err := service.GetConstants(context.Background(), "heroes"); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 2 {
		t.Fatalf("after TTL expiry: calls = %d, want 2", executor.calls)
	}
}

func TestHeroNamesResolvesIDsFromConstants(t *testing.T) {
	executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(constantsFixture), nil
	}}
	service := mustService(t, executor)
	names, _, err := service.HeroNames(context.Background(), []int64{1, 2, 99}, service.budget())
	if err != nil {
		t.Fatal(err)
	}
	if names[1] != "Axe" || names[2] != "Queen of Pain" {
		t.Fatalf("names = %#v", names)
	}
	if _, ok := names[99]; ok {
		t.Fatalf("unknown hero id 99 was present in names")
	}
	// Empty input skips the upstream request entirely.
	executor.calls = 0
	empty, _, err := service.HeroNames(context.Background(), nil, service.budget())
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty HeroNames = %#v, err = %v", empty, err)
	}
	if executor.calls != 0 {
		t.Fatalf("empty HeroNames made %d upstream calls", executor.calls)
	}
}

func TestConstantsCacheHitReportsNoRateLimits(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return &stratz.Response{
			Data:       json.RawMessage(constantsFixture),
			RateLimits: []stratz.RateLimit{{Window: "minute", Remaining: int64Ptr(120)}},
		}, nil
	}}
	service, err := New(Options{
		Executor: executor, MaxUpstreamRequests: 5,
		ConstantsTTL: time.Hour, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Cold: the live fetch carries rate limits.
	first, err := service.GetConstants(context.Background(), "heroes")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.RateLimits) == 0 {
		t.Fatalf("cold fetch should report rate limits")
	}
	// Warm hit: no upstream request was made, so rate limits must not be
	// reported (they would be stale quota telemetry, not fresh data).
	hit, err := service.GetConstants(context.Background(), "heroes")
	if err != nil {
		t.Fatal(err)
	}
	if len(hit.RateLimits) != 0 {
		t.Fatalf("cache hit reported stale rate limits %#v", hit.RateLimits)
	}
}

// TestConstantsColdCacheCoalescesConcurrentFetches proves that a burst of
// callers on a cold cache is coalesced into a single upstream fetch instead of
// one fetch per caller. The gate forces every caller to be in flight at once
// (a thundering herd without singleflight), so the assertion is deterministic.
func TestConstantsColdCacheCoalescesConcurrentFetches(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)

	var calls int64
	gate := make(chan struct{})
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, _ stratz.Request) (*stratz.Response, error) {
		atomic.AddInt64(&calls, 1)
		<-gate // block until all callers have piled up on the cold cache
		return response(constantsFixture), nil
	}}
	service, err := New(Options{
		Executor:            executor,
		MaxUpstreamRequests: 5,
		ConstantsTTL:        time.Hour,
		Now:                 func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 16
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	start := make(chan struct{})
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = service.GetConstants(context.Background(), "heroes")
		}(i)
	}
	close(start)                      // every goroutine races toward loadConstants
	time.Sleep(20 * time.Millisecond) // let the herd pile up on the cold cache
	close(gate)                       // release the single coalesced fetch

	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d error = %v", i, err)
		}
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (cold-cache thundering herd not coalesced)", got)
	}
}

func mustService(t *testing.T, executor stratz.Executor) *Service {
	return mustServiceAt(t, executor, time.Now())
}

func mustServiceAt(t *testing.T, executor stratz.Executor, now time.Time) *Service {
	t.Helper()
	service, err := New(Options{
		Executor: executor, MaxUpstreamRequests: 5, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func response(data string) *stratz.Response {
	return &stratz.Response{Data: json.RawMessage(data)}
}

func int64Ptr(value int64) *int64 { return &value }

func assertCode(t *testing.T, err error, want contracts.ErrorCode) {
	t.Helper()
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != want {
		t.Fatalf("error = %#v, want %s", err, want)
	}
}

const constantsFixture = `{"constants":{
	"heroes":[
		{"id":1,"name":"npc_dota_hero_axe","localizedName":"Axe","primaryAttribute":"strength","attackType":"melee","roles":["Initiator"]},
		{"id":2,"name":"npc_dota_hero_queenofpain","localizedName":"Queen of Pain","primaryAttribute":"intelligence","attackType":"ranged","roles":["Carry"]},
		{"id":3,"name":"npc_dota_hero_twin_a","localizedName":"Twin","roles":[]},
		{"id":4,"name":"npc_dota_hero_twin_b","localizedName":"Twin","roles":[]}
	],
	"items":[{"id":"1","name":"blink","localizedName":"Blink Dagger","metadata":[{"key":"cost","value":"2250"}]}],
	"abilities":[{"id":"10","name":"berserkers_call","localizedName":"Berserker's Call","metadata":[]}],
	"gameModes":[{"id":"22","name":"all_pick","localizedName":"All Pick","metadata":[]}],
	"regions":[{"id":"3","name":"europe","localizedName":"Europe","metadata":[]}],
	"ranks":[]
}}`
