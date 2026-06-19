package leaguelive

import (
	"context"
	"encoding/json"
	"fmt"
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
	result, err := service.ListLeagues(context.Background(), LeagueFilters{Query: &query, Status: &status, Tier: &tier, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 5 || len(result.Data.Items) != 0 || result.Data.Page.NextCursor == nil || len(result.Warnings) != 1 {
		t.Fatalf("result = %#v calls=%d", result, executor.calls)
	}
	cursor := *result.Data.Page.NextCursor
	executor.calls = 0
	resumed, err := service.ListLeagues(context.Background(), LeagueFilters{Query: &query, Status: &status, Tier: &tier, Limit: 3, Cursor: cursor})
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
	if first.Data.Items[0].ParseStatus != "parsed" || first.Data.Items[1].ParseStatus != "unparsed" || first.Data.Page.NextCursor == nil {
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

func TestLiveNativeAndClientFiltersWithIncompleteSnapshotWarning(t *testing.T) {
	executor := &fixtureExecutor{execute: func(request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzListLiveMatches" {
			t.Fatalf("operation = %q", request.OperationName)
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
