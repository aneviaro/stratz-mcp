package heroconstants

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
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

func TestHeroStatisticsBucketsRatesRelationsAndRankUnavailable(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetHeroStatsWeek" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		variables := request.Variables.(map[string]any)["request"].(map[string]any)
		if variables["bucket"] != "week" ||
			variables["startDateTime"] != time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC).Unix() ||
			variables["endDateTime"] != time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC).Unix() {
			t.Fatalf("variables = %#v", variables)
		}
		return response(`{"heroStats":{
			"heroId":1,"matchCount":40,"pickCount":40,"winCount":25,"banCount":10,"populationMatchCount":100,
			"rankDataAvailable":true,
			"roles":[{"name":"Core","matchCount":30,"pickCount":30,"winCount":18,"populationMatchCount":100}],
			"lanes":[{"name":"Mid","matchCount":20,"pickCount":20,"winCount":12,"populationMatchCount":100}],
			"matchups":[{"heroId":2,"matchCount":10,"winCount":7,"expectedWinRate":0.5}],
			"synergies":[{"heroId":3,"matchCount":8,"winCount":5,"expectedWinRate":0.55}]
		}}`), nil
	}}
	service := mustServiceAt(t, executor, now)
	from := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	result, err := service.GetHeroStats(context.Background(), StatsFilters{
		Hero: json.Number("1"), From: &from, To: &to, IncludeMatchups: true, IncludeSynergies: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EffectiveRange == nil ||
		result.Data.PickRate == nil || math.Abs(*result.Data.PickRate-0.4) > 0.000001 ||
		result.Data.WinRate == nil || math.Abs(*result.Data.WinRate-0.625) > 0.000001 ||
		result.Data.Matchups[0].Advantage == nil || math.Abs(*result.Data.Matchups[0].Advantage-0.2) > 0.000001 {
		t.Fatalf("result = %#v", result)
	}

	rankExecutor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(`{"heroStats":{
			"heroId":1,"matchCount":40,"pickCount":40,"winCount":25,"banCount":10,"populationMatchCount":100,
			"rankDataAvailable":false,"roles":[],"lanes":[],"matchups":[],"synergies":[]
		}}`), nil
	}}
	rank := "immortal"
	rankResult, err := mustServiceAt(t, rankExecutor, now).GetHeroStats(
		context.Background(),
		StatsFilters{Hero: json.Number("1"), RankBracket: &rank},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankResult.Warnings) != 1 || rankResult.Data.WinRate != nil || rankResult.Data.SampleSize != 40 {
		t.Fatalf("rank result = %#v", rankResult)
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
