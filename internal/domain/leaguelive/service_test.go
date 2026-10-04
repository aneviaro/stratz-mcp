package leaguelive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

type fixtureExecutor struct {
	calls   int
	execute func(stratz.Request) (*stratz.Response, error)
}

func (executor *fixtureExecutor) Execute(_ context.Context, budget *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
	executor.calls++
	if !budget.Take() {
		return nil, &stratz.Error{Code: contracts.ErrorCodeRequestBudgetExceeded, Message: "budget exhausted", Details: map[string]any{}}
	}
	return executor.execute(request)
}

func TestLeagueMappingDerivesDeterministicStatus(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	earlier, later := now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix()
	trueValue := true
	cases := []struct {
		name   string
		league upstreamLeague
		want   string
	}{
		{"live flag wins", upstreamLeague{IsLive: &trueValue, IsEnded: &trueValue}, "live"},
		{"ended flag", upstreamLeague{IsEnded: &trueValue}, "completed"},
		{"future flag", upstreamLeague{IsFuture: &trueValue}, "upcoming"},
		{"past end", upstreamLeague{EndDateTime: &earlier}, "completed"},
		{"future start", upstreamLeague{StartDateTime: &later}, "upcoming"},
		{"started", upstreamLeague{StartDateTime: &earlier, EndDateTime: &later}, "ongoing"},
		{"undated", upstreamLeague{}, "unknown"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveStatus(&test.league, now); got != test.want {
				t.Fatalf("deriveStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFetchLeagueMapsSuccessAndNotFound(t *testing.T) {
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetLeague" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		if request.Variables.(map[string]any)["id"] != int64(42) {
			t.Fatalf("variables = %#v", request.Variables)
		}
		return response(`{"league":{"id":42,"name":"fixture","displayName":"Fixture League","isLive":true}}`), nil
	}}
	result, err := mustService(t, executor).FetchLeague(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.LeagueID != "42" || result.Data.Name != "fixture" ||
		result.Data.Status == nil || *result.Data.Status != "live" {
		t.Fatalf("league = %#v", result.Data)
	}

	executor.execute = func(stratz.Request) (*stratz.Response, error) {
		return response(`{"league":null}`), nil
	}
	if _, err := mustService(t, executor).FetchLeague(context.Background(), "42"); err == nil {
		t.Fatal("expected not-found error")
	} else if domainErr, ok := err.(*Error); !ok || domainErr.Code != contracts.ErrorCodeNotFound {
		t.Fatalf("not-found error = %#v", err)
	}
}

func TestLeagueNameSearchStopsAtFivePagesAndResumes(t *testing.T) {
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzListLeagues" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		variables := request.Variables.(map[string]any)
		native := variables["request"].(map[string]any)
		skip := native["skip"].(int64)
		if native["tiers"].([]string)[0] != "PROFESSIONAL" || native["isEnded"] != true {
			t.Fatalf("native request = %#v", native)
		}
		items := make([]string, 20)
		for index := range items {
			id := skip + int64(index) + 1
			items[index] = fmt.Sprintf(`{"id":%d,"name":"League %d"}`, id, id)
		}
		return response(`{"leagues":[` + strings.Join(items, ",") + `]}`), nil
	}}
	service := mustService(t, executor)
	query, status, tier := "International", "completed", "PROFESSIONAL"
	firstRequest := map[string]any{
		"mode": "search", "query": query, "status": status, "tier": tier,
		"limit": 3, "fresh": false,
	}
	result, err := service.QueryLeagues(context.Background(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 5 || len(result.Data.Items) != 0 || result.Data.Page.NextCursor == nil || len(result.Warnings) != 1 {
		t.Fatalf("result = %#v calls=%d", result, executor.calls)
	}
	cursor := *result.Data.Page.NextCursor
	executor.calls = 0
	resumed, err := service.QueryLeagues(context.Background(), map[string]any{
		"mode": "search", "query": query, "status": status, "tier": tier,
		"limit": 3, "cursor": cursor, "fresh": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 5 || resumed.Data.Page.NextCursor == nil {
		t.Fatalf("resumed = %#v calls=%d", resumed, executor.calls)
	}
}

func TestLeagueMatchesUsesNativeFiltersAndContinuation(t *testing.T) {
	call := 0
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		call++
		if request.OperationName != "StratzListLeagueMatches" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		variables := request.Variables.(map[string]any)
		native := variables["request"].(map[string]any)
		wantSkip := int64((call - 1) * 2)
		if variables["id"] != int64(42) || native["skip"] != wantSkip || native["gameVersionIds"].([]string)[0] != "7.39" {
			t.Fatalf("variables = %#v", variables)
		}
		return response(fmt.Sprintf(`{"league":{"id":42,"matches":[
			{"id":%d,"leagueId":42,"parsedDateTime":1},
			{"id":%d,"leagueId":42}
		]}}`, wantSkip+1, wantSkip+2)), nil
	}}
	service := mustService(t, executor)
	patch := "7.39"
	first, err := service.ListLeagueMatches(context.Background(), LeagueMatchFilters{LeagueID: "42", PatchID: &patch, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Data.Items[0].ParseStatus != "parsed" || first.Data.Items[1].ParseStatus != "pending" || first.Data.Page.NextCursor == nil {
		t.Fatalf("first = %#v", first.Data)
	}
	second, err := service.ListLeagueMatches(context.Background(), LeagueMatchFilters{
		LeagueID: "42", PatchID: &patch, Limit: 2, Cursor: *first.Data.Page.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Data.Items[0].MatchID != "3" {
		t.Fatalf("second = %#v", second.Data)
	}
}

func TestQueryMatchesRoutesLeagueHistoryAndLiveBranches(t *testing.T) {
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		switch request.OperationName {
		case "StratzListLeagueMatches":
			return response(`{"league":{"id":42,"matches":[{"id":4,"radiantKills":[1,2],"direKills":[1],"parsedDateTime":1}]}}`), nil
		case "StratzListLiveMatches":
			return response(`{"live":{"matches":[{"id":5,"spectatorCount":500,"gameModeId":22,"players":[{"steamAccountId":1,"heroId":7,"isRadiant":true,"kills":3,"deaths":1,"assists":2}]}]}}`), nil
		default:
			t.Fatalf("operation = %q", request.OperationName)
			return nil, nil
		}
	}}
	service := mustService(t, executor)
	league, err := service.QueryMatches(context.Background(), map[string]any{"mode": "league_history", "league_id": "42", "limit": 1})
	if err != nil || league.Data.Mode != "league_history" {
		t.Fatalf("league result = %#v, err = %v", league, err)
	}
	leagueItems, ok := league.Data.Items.([]contracts.MatchSummary)
	if !ok || len(leagueItems) != 1 || leagueItems[0].RadiantScore == nil || *leagueItems[0].RadiantScore != 2 {
		t.Fatalf("league items = %#v", league.Data.Items)
	}
	live, err := service.QueryMatches(context.Background(), map[string]any{"mode": "live", "player_id": "1", "minimum_spectators": 100, "limit": 1})
	if err != nil || live.Data.Mode != "live" {
		t.Fatalf("live result = %#v, err = %v", live, err)
	}
	liveItems, ok := live.Data.Items.([]contracts.LiveMatch)
	if !ok || len(liveItems) != 1 || len(liveItems[0].Players) != 1 || liveItems[0].Players[0].Kills != 3 {
		t.Fatalf("live items = %#v", live.Data.Items)
	}
	for _, detail := range []string{"standard", "players", "full"} {
		if _, err := service.QueryMatches(context.Background(), map[string]any{
			"mode": "league_history", "league_id": "42", "detail_level": detail,
		}); err == nil {
			t.Fatalf("%s league-history detail unexpectedly accepted", detail)
		}
	}
}

func TestQueryMatchesLeagueHistoryContinuationUsesReturnedCursor(t *testing.T) {
	calls := 0
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		calls++
		if request.OperationName != "StratzListLeagueMatches" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		variables := request.Variables.(map[string]any)
		if variables["id"] != int64(42) {
			t.Fatalf("variables = %#v", variables)
		}
		native := variables["request"].(map[string]any)
		if native["skip"] != int64(calls-1) || native["take"] != 1 {
			t.Fatalf("native request = %#v", native)
		}
		return response(fmt.Sprintf(`{"league":{"id":42,"matches":[{"id":%d}]}}`, calls)), nil
	}}
	service := mustService(t, executor)
	request := map[string]any{"mode": "league_history", "league_id": "42", "limit": 1}
	first, err := service.QueryMatches(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data.Items.([]contracts.MatchSummary)) != 1 || first.Data.Page == nil || first.Data.Page.NextCursor == nil {
		t.Fatalf("first result = %#v", first.Data)
	}

	request["cursor"] = *first.Data.Page.NextCursor
	second, err := service.QueryMatches(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := second.Data.Items.([]contracts.MatchSummary)
	if !ok || len(items) != 1 || items[0].MatchID != "2" {
		t.Fatalf("second result = %#v", second.Data)
	}
}

func TestListLeaguesRejectsUnsupportedStatus(t *testing.T) {
	executor := &fixtureExecutor{execute: func(stratz.Request) (*stratz.Response, error) {
		t.Fatal("upstream should not be called")
		return nil, nil
	}}
	service := mustService(t, executor)
	status := "typo"
	_, err := service.ListLeagues(context.Background(), LeagueFilters{Status: &status, Limit: 20})
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != contracts.ErrorCodeInvalidArgument {
		t.Fatalf("error = %#v", err)
	}
}

func TestMapLivePreservesUnknownHeroAndOutcome(t *testing.T) {
	mapped := mapLive(&upstreamLiveMatch{
		ID: 1,
		Players: []upstreamLivePlayer{{
			HeroID: 0, IsRadiant: true, PlayerSlot: 1,
		}},
	}, time.Now())
	if len(mapped.Players) != 1 ||
		mapped.Players[0].HeroID != nil ||
		mapped.Players[0].Won != nil {
		t.Fatalf("players = %#v", mapped.Players)
	}
}

func TestLeagueServiceMapsUpstreamAndProtocolFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		response *stratz.Response
		err      error
		want     contracts.ErrorCode
	}{
		{
			name: "upstream",
			err: &stratz.Error{
				Code: contracts.ErrorCodeRateLimited, Message: "limited",
				Details: map[string]any{},
			},
			want: contracts.ErrorCodeRateLimited,
		},
		{name: "nil response", want: contracts.ErrorCodeUpstreamProtocolError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := mustService(t, &fixtureExecutor{execute: func(stratz.Request) (*stratz.Response, error) {
				return test.response, test.err
			}})
			_, err := service.FetchLeague(context.Background(), "1")
			var domainErr *Error
			if !errors.As(err, &domainErr) || domainErr.Code != test.want {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestLiveNativeAndClientFiltersWithIncompleteSnapshotWarning(t *testing.T) {
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzListLiveMatches" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		for _, selection := range []string{
			"id: matchId",
			"startDateTime: createdDateTime",
			"gameModeId: gameMode",
			"spectatorCount: spectators",
			"kills: numKills",
			"deaths: numDeaths",
			"assists: numAssists",
		} {
			if !strings.Contains(request.Query, selection) {
				t.Fatalf("live query is missing %q: %s", selection, request.Query)
			}
		}
		for _, obsolete := range []string{
			"radiantTeamName",
			"direTeamName",
			"isFuture",
			"isEnded",
			"isLive",
		} {
			if strings.Contains(request.Query, obsolete) {
				t.Fatalf("live query contains obsolete selection %q: %s", obsolete, request.Query)
			}
		}
		native := request.Variables.(map[string]any)["request"].(map[string]any)
		if native["orderBy"] != "SPECTATOR_COUNT" ||
			native["leagueIds"].([]int64)[0] != 9 ||
			native["heroIds"].([]int64)[0] != 1 ||
			native["gameStates"].([]string)[0] != "GAME_IN_PROGRESS" {
			t.Fatalf("native request = %#v", native)
		}
		if _, exists := native["teamIds"]; exists {
			t.Fatal("team filter must remain client-side")
		}
		items := make([]string, 20)
		for index := range items {
			items[index] = fmt.Sprintf(`{"id":%d,"gameModeId":22,"spectatorCount":500,
				"radiantTeamId":7,"players":[{"steamAccountId":11,"heroId":1,"isRadiant":true,
				"playerSlot":0,"kills":1,"deaths":0,"assists":2}]}`, index+1)
		}
		return response(`{"live":{"matches":[` + strings.Join(items, ",") + `]}}`), nil
	}}
	service := mustService(t, executor)
	player, team, league, hero, mode, spectators := int64(11), int64(7), int64(9), int64(1), int64(22), int64(100)
	result, err := service.ListLiveMatches(context.Background(), LiveFilters{
		PlayerID: &player, TeamID: &team, LeagueID: &league, HeroID: &hero,
		GameStates: []string{"GAME_IN_PROGRESS"}, Tiers: []string{"PROFESSIONAL"},
		GameModeID: &mode, MinimumSpectators: &spectators, Sort: "highest_profile", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Items) != 2 || result.Data.Page.NextCursor == nil || len(result.Warnings) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(*result.Data.Page.NextCursor) > 4096 {
		t.Fatalf("cursor length = %d, want at most 4096", len(*result.Data.Page.NextCursor))
	}
	if result.Data.Items[0].Players[0].AccountID == nil || *result.Data.Items[0].Players[0].AccountID != "11" {
		t.Fatalf("mapped item = %#v", result.Data.Items[0])
	}
}

func TestLiveNewestSortAndUnsupportedRegionContract(t *testing.T) {
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		native := request.Variables.(map[string]any)["request"].(map[string]any)
		if native["orderBy"] != "MATCH_ID" {
			t.Fatalf("orderBy = %#v", native["orderBy"])
		}
		return response(`{"live":{"matches":[]}}`), nil
	}}
	_, err := mustService(t, executor).ListLiveMatches(context.Background(), LiveFilters{Sort: "newest", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateInput("stratz_list_live_matches", map[string]any{"region_id": json.Number("1")}); err == nil {
		t.Fatal("region_id unexpectedly accepted by live-match contract")
	}
}

func TestQueryLeaguesExactAndBoundedSearch(t *testing.T) {
	calls := 0
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		calls++
		input := request.Variables.(map[string]any)["request"].(map[string]any)
		if calls == 1 {
			if !reflect.DeepEqual(input["leagueIds"], []int64{1, 2}) {
				t.Fatalf("exact native IDs = %#v", input["leagueIds"])
			}
			return response(`{"leagues":[{"id":2,"name":"two"},{"id":1,"name":"one"}]}`), nil
		}
		if input["isEnded"] != true || input["tiers"].([]string)[0] != "PROFESSIONAL" {
			t.Fatalf("search native filters = %#v", input)
		}
		return response(`{"leagues":[{"id":9,"name":"Target League"}]}`), nil
	}}
	service := mustService(t, executor)
	exact, err := service.QueryLeagues(context.Background(), map[string]any{
		"mode": "exact", "league_ids": []any{"1", "2", "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{exact.Data.Items[0].LeagueID, exact.Data.Items[1].LeagueID, exact.Data.Items[2].LeagueID}; !reflect.DeepEqual(got, []string{"1", "2", "1"}) {
		t.Fatalf("exact items = %#v", got)
	}
	search, err := service.QueryLeagues(context.Background(), map[string]any{
		"mode": "search", "query": "target", "status": "completed", "tier": "PROFESSIONAL", "limit": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Data.Items) != 1 || search.Data.Items[0].LeagueID != "9" || search.Data.Page == nil || search.Data.Page.HasMore {
		t.Fatalf("search result = %#v", search.Data)
	}
}

func mustService(t *testing.T, executor stratz.Executor) *Service {
	t.Helper()
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	service, err := New(Options{
		Executor: executor, Token: "test-token", SchemaVersion: "schema-v1",
		MaxUpstreamRequests: 5, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func response(data string) *stratz.Response {
	return &stratz.Response{Data: json.RawMessage(data)}
}
