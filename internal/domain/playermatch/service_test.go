package playermatch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
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
			Code:    contracts.ErrorCodeRequestBudgetExceeded,
			Message: "budget exhausted",
			Details: map[string]any{},
		}
	}
	return executor.execute(budget, request)
}

func TestNormalizePlayerIDForms(t *testing.T) {
	want := PlayerID{AccountID: 39734272, SteamID64: 76561198000000000}
	for _, input := range []string{
		"39734272",
		"76561198000000000",
		"https://stratz.com/players/39734272",
		"https://www.stratz.com/players/76561198000000000?tab=matches",
	} {
		got, err := NormalizePlayerID(input)
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if got != want {
			t.Fatalf("%s normalized to %#v, want %#v", input, got, want)
		}
	}
	for _, input := range []string{
		"",
		"abc",
		"http://stratz.com/players/39734272",
		"https://example.com/players/39734272",
		"https://stratz.com/player/39734272",
		"76561197960265727",
	} {
		if _, err := NormalizePlayerID(input); err == nil {
			t.Fatalf("NormalizePlayerID(%q) succeeded", input)
		}
	}
}

func TestGetPlayerMappingPrivateMissingAndPartial(t *testing.T) {
	t.Run("mapped", func(t *testing.T) {
		executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
			if request.OperationName != "StratzGetPlayer" {
				t.Fatalf("operation = %q", request.OperationName)
			}
			return response(`{"player":{
				"steamAccountId":39734272,
				"steamAccount":{"name":" Example\u0000 ","avatar":"https://cdn.example/avatar.png"},
				"identity":{"name":"Profile"},
				"matchCount":1200,
				"winCount":620,
				"lastMatchDate":1781724600,
				"ranks":[{"rank":65,"leaderboardRank":null}]
			}}`), nil
		}}
		result, err := mustService(t, executor, 5).GetPlayer(context.Background(), "76561198000000000")
		if err != nil {
			t.Fatal(err)
		}
		if result.Data.AccountID != "39734272" ||
			result.Data.SteamID64 == nil ||
			*result.Data.SteamID64 != "76561198000000000" ||
			result.Data.DisplayName == nil ||
			*result.Data.DisplayName != "Profile" ||
			result.Data.Rank == nil ||
			result.Data.Rank.RankTier == nil ||
			*result.Data.Rank.RankTier != 65 {
			t.Fatalf("player = %#v", result.Data)
		}
	})

	t.Run("missing", func(t *testing.T) {
		executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
			return response(`{"player":null}`), nil
		}}
		_, err := mustService(t, executor, 5).GetPlayer(context.Background(), "1")
		assertCode(t, err, contracts.ErrorCodeNotFound)
	})

	t.Run("private", func(t *testing.T) {
		executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
			return nil, &stratz.Error{
				Code:    contracts.ErrorCodePrivate,
				Message: "private",
				Details: map[string]any{},
			}
		}}
		_, err := mustService(t, executor, 5).GetPlayer(context.Background(), "1")
		assertCode(t, err, contracts.ErrorCodePrivate)
	})

	t.Run("partial", func(t *testing.T) {
		executor := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
			return nil, &stratz.Error{
				Code:    contracts.ErrorCodeUpstreamPartialError,
				Message: "partial",
				Details: map[string]any{"graphql_codes": []string{"PARTIAL"}},
			}
		}}
		_, err := mustService(t, executor, 5).GetPlayer(context.Background(), "1")
		assertCode(t, err, contracts.ErrorCodeUpstreamPartialError)
	})
}

func TestBatchPlayersUsesNativeOperationAndRestoresDuplicates(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		variables := request.Variables.(map[string]any)
		if request.OperationName != "StratzGetPlayers" ||
			!reflect.DeepEqual(variables["steamAccountIds"], []int64{1, 2}) {
			t.Fatalf("request = %#v", request)
		}
		return response(`{"players":[
			{"steamAccountId":2,"steamAccount":null,"identity":null,"ranks":[]},
			{"steamAccountId":1,"steamAccount":null,"identity":null,"ranks":[]}
		]}`), nil
	}}
	result, err := mustService(t, executor, 5).BatchPlayers(
		context.Background(),
		[]string{"1", "2", "1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("calls = %d, want 1", executor.calls)
	}
	got := []string{result.Data[0].AccountID, result.Data[1].AccountID, result.Data[2].AccountID}
	if want := []string{"1", "2", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %#v, want %#v", got, want)
	}

	missing := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(`{"players":[{"steamAccountId":1,"ranks":[]}]}`), nil
	}}
	_, err = mustService(t, missing, 5).BatchPlayers(context.Background(), []string{"1", "2"})
	var domainErr *Error
	if !errors.As(err, &domainErr) ||
		domainErr.Code != contracts.ErrorCodeNotFound ||
		domainErr.FailedInput == nil {
		t.Fatalf("error = %#v", err)
	}
}

func TestBatchPlayersHonorsConfiguredMaximum(t *testing.T) {
	service, err := New(Options{
		Executor:            &fixtureExecutor{},
		Token:               "test-token",
		SchemaVersion:       "schema-v1",
		MaxUpstreamRequests: 5,
		MaxBatchSize:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BatchPlayers(context.Background(), []string{"1", "2", "3"}); err == nil {
		t.Fatal("batch accepted more than the configured maximum")
	}
}

func TestMatchDetailLevelsAndDataNotReady(t *testing.T) {
	payload := `{"match":{
		"id":8000000000,
		"startDateTime":1781724600,
		"durationSeconds":2430,
		"didRadiantWin":true,
		"radiantKills":42,
		"direKills":31,
		"gameModeId":22,
		"lobbyTypeId":7,
		"regionId":3,
		"leagueId":null,
		"gameVersionId":"7.XX",
		"parsedDateTime":1781728000,
		"players":[{"steamAccountId":39734272,"heroId":5,"isRadiant":true,"playerSlot":0,"kills":10,"deaths":2,"assists":15,"networth":20000,"level":25}],
		"playbackData":{
			"buildingEvents":[{"time":100,"type":"TOWER","isRadiant":true,"npcId":42}],
			"roshanEvents":[{"time":200}],
			"towerDeathEvents":[{"time":300,"radiant":1,"dire":2}],
			"runeEvents":[{"time":50,"action":1,"rune":2}],
			"wardEvents":[{"time":60,"action":"SPAWN","wardType":"OBSERVER","fromPlayer":0}]
		}
	}}`
	for _, test := range []struct {
		detail            contracts.DetailLevel
		wantObjectives    bool
		wantFull          bool
		wantOperationName string
	}{
		{contracts.DetailLevelSummary, false, false, "StratzGetMatchSummary"},
		{contracts.DetailLevelStandard, true, false, "StratzGetMatchStandard"},
		{contracts.DetailLevelFull, true, true, "StratzGetMatchFull"},
	} {
		t.Run(string(test.detail), func(t *testing.T) {
			executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
				if request.OperationName != test.wantOperationName {
					t.Fatalf("operation = %q", request.OperationName)
				}
				if test.wantObjectives && !strings.Contains(request.Query, "buildingEvents") {
					t.Fatalf("standard query is missing playback objectives: %s", request.Query)
				}
				if test.wantFull && (!strings.Contains(request.Query, "runeEvents") ||
					!strings.Contains(request.Query, "wardEvents")) {
					t.Fatalf("full query is missing playback timeline: %s", request.Query)
				}
				return response(payload), nil
			}}
			result, err := mustService(t, executor, 5).GetMatch(context.Background(), "8000000000", test.detail)
			if err != nil {
				t.Fatal(err)
			}
			if (result.Data.Objectives != nil) != test.wantObjectives ||
				(result.Data.Timeline != nil) != test.wantFull {
				t.Fatalf("match detail mapping = %#v", result.Data)
			}
			if test.wantObjectives && len(result.Data.Objectives) != 3 {
				t.Fatalf("objectives = %#v", result.Data.Objectives)
			}
			if test.wantFull && len(result.Data.Timeline) != 2 {
				t.Fatalf("timeline = %#v", result.Data.Timeline)
			}
			if test.wantFull && result.Data.Timeline[1].Value != "ward_type=OBSERVER;from_player=0" {
				t.Fatalf("ward value = %#v", result.Data.Timeline[1].Value)
			}
			if test.wantFull {
				if !reflect.DeepEqual(result.Warnings, []string{fullMatchAvailabilityWarning}) {
					t.Fatalf("warnings = %#v", result.Warnings)
				}
			} else if len(result.Warnings) != 0 {
				t.Fatalf("warnings = %#v", result.Warnings)
			}
		})
	}

	pending := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(`{"match":{"id":9,"parseStatus":"pending","players":[]}}`), nil
	}}
	_, err := mustService(t, pending, 5).GetMatch(context.Background(), "9", contracts.DetailLevelFull)
	var domainErr *Error
	if !errors.As(err, &domainErr) ||
		domainErr.Code != contracts.ErrorCodeDataNotReady ||
		domainErr.Context == nil ||
		domainErr.Details["parse_status"] != "pending" {
		t.Fatalf("error = %#v", err)
	}

	missingPlayback := &fixtureExecutor{execute: func(*stratz.RequestBudget, stratz.Request) (*stratz.Response, error) {
		return response(`{"match":{"id":10,"parsedDateTime":10,"players":[],"playbackData":null}}`), nil
	}}
	_, err = mustService(t, missingPlayback, 5).GetMatch(
		context.Background(),
		"10",
		contracts.DetailLevelStandard,
	)
	if !errors.As(err, &domainErr) ||
		domainErr.Code != contracts.ErrorCodeDataNotReady ||
		domainErr.Details["missing"] != "playback_data" {
		t.Fatalf("missing playback error = %#v", err)
	}
}

func TestListPlayerMatchesBoundedContinuation(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		variables := request.Variables.(map[string]any)
		requestInput := variables["request"].(map[string]any)
		skip := requestInput["skip"].(int64)
		matches := make([]map[string]any, 20)
		for index := range matches {
			duration := int64(100)
			if (skip == 0 && index == 3) || (skip == 20 && index == 4) {
				duration = 1200
			}
			matches[index] = map[string]any{
				"id":              skip + int64(index) + 1,
				"durationSeconds": duration,
				"parseStatus":     "parsed",
			}
		}
		data, _ := json.Marshal(map[string]any{
			"player": map[string]any{
				"steamAccountId": 1,
				"matches":        matches,
			},
		})
		return &stratz.Response{HTTPStatus: 200, Data: data}, nil
	}}
	service := mustService(t, executor, 1)
	minimum := int64(1000)
	first, err := service.ListPlayerMatches(context.Background(), PlayerMatchFilters{
		PlayerID:               "1",
		Limit:                  2,
		MinimumDurationSeconds: &minimum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data.Items) != 1 ||
		first.Data.Page.NextCursor == nil ||
		!first.Data.Page.HasMore {
		t.Fatalf("first page = %#v", first.Data)
	}
	second, err := service.ListPlayerMatches(context.Background(), PlayerMatchFilters{
		PlayerID:               "1",
		Limit:                  2,
		MinimumDurationSeconds: &minimum,
		Cursor:                 *first.Data.Page.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Data.Items) != 1 || executor.calls != 2 {
		t.Fatalf("second page = %#v, calls = %d", second.Data, executor.calls)
	}
}

func TestListPlayerMatchesUsesCurrentGameModeAndLobbyTypeFilters(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if !strings.Contains(request.Query, "gameModeId: gameMode") ||
			!strings.Contains(request.Query, "lobbyTypeId: lobbyType") {
			t.Fatalf("match query does not alias current STRATZ fields: %s", request.Query)
		}
		variables := request.Variables.(map[string]any)
		requestInput := variables["request"].(map[string]any)
		if got := requestInput["gameModeIds"]; !reflect.DeepEqual(got, []int64{22}) {
			t.Fatalf("gameModeIds = %#v, want [22]", got)
		}
		if got := requestInput["lobbyTypeIds"]; !reflect.DeepEqual(got, []int64{7}) {
			t.Fatalf("lobbyTypeIds = %#v, want [7]", got)
		}
		if _, exists := requestInput["gameMode"]; exists {
			t.Fatalf("obsolete gameMode was sent: %#v", requestInput)
		}
		if _, exists := requestInput["lobbyType"]; exists {
			t.Fatalf("obsolete lobbyType was sent: %#v", requestInput)
		}
		return response(`{"player":{"steamAccountId":1,"matches":[]}}`), nil
	}}
	gameModeID := int64(22)
	lobbyTypeID := int64(7)
	_, err := mustService(t, executor, 5).ListPlayerMatches(
		context.Background(),
		PlayerMatchFilters{
			PlayerID:    "1",
			Limit:       20,
			GameModeID:  &gameModeID,
			LobbyTypeID: &lobbyTypeID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBatchMatchesIsAtomicForDataNotReady(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetMatchBatchStandard" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		return response(`{
			"match0":{"id":1,"parsedDateTime":10,"players":[],"playbackData":{}},
			"match1":{"id":2,"parseStatus":"pending","players":[]}
		}`), nil
	}}
	_, err := mustService(t, executor, 5).BatchMatches(
		context.Background(),
		[]string{"1", "2"},
		contracts.DetailLevelStandard,
	)
	var domainErr *Error
	if !errors.As(err, &domainErr) ||
		domainErr.Code != contracts.ErrorCodeDataNotReady ||
		domainErr.FailedInput != "2" {
		t.Fatalf("error = %#v", err)
	}
}

func TestBatchMatchesUsesPublicSingleMatchQueries(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetMatchBatchSummary" ||
			strings.Contains(request.Query, "matches(ids:") {
			t.Fatalf("batch used non-public operation: %q\n%s", request.OperationName, request.Query)
		}
		variables := request.Variables.(map[string]any)
		return response(`{
			"match0":{"id":` + strconv.FormatInt(variables["id0"].(int64), 10) + `,"players":[]},
			"match1":{"id":` + strconv.FormatInt(variables["id1"].(int64), 10) + `,"players":[]}
		}`), nil
	}}
	result, err := mustService(t, executor, 5).BatchMatches(
		context.Background(),
		[]string{"2", "1", "2"},
		contracts.DetailLevelSummary,
	)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 ||
		len(result.Data) != 3 ||
		result.Data[0].MatchID != "2" ||
		result.Data[1].MatchID != "1" ||
		result.Data[2].MatchID != "2" {
		t.Fatalf("result = %#v, calls = %d", result.Data, executor.calls)
	}
}

func TestBatchMatchesFitsTwentyFiveIDsWithinRequestBudget(t *testing.T) {
	executor := &fixtureExecutor{execute: func(_ *stratz.RequestBudget, request stratz.Request) (*stratz.Response, error) {
		if request.OperationName != "StratzGetMatchBatchSummary" {
			t.Fatalf("operation = %q", request.OperationName)
		}
		variables := request.Variables.(map[string]any)
		payload := make(map[string]any, 5)
		for index := range 5 {
			id := variables["id"+strconv.Itoa(index)].(int64)
			payload["match"+strconv.Itoa(index)] = map[string]any{"id": id, "players": []any{}}
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return response(string(encoded)), nil
	}}
	ids := make([]string, 25)
	for index := range ids {
		ids[index] = strconv.Itoa(index + 1)
	}
	result, err := mustService(t, executor, 5).BatchMatches(
		context.Background(),
		ids,
		contracts.DetailLevelSummary,
	)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 5 || len(result.Data) != 25 {
		t.Fatalf("calls = %d, items = %d", executor.calls, len(result.Data))
	}
}

func mustService(t *testing.T, executor stratz.Executor, maximum int) *Service {
	t.Helper()
	service, err := New(Options{
		Executor:            executor,
		Token:               "fixture-token",
		SchemaVersion:       "sha256:fixture",
		MaxUpstreamRequests: maximum,
		Now: func() time.Time {
			return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func response(data string) *stratz.Response {
	return &stratz.Response{HTTPStatus: 200, Data: json.RawMessage(data)}
}

func assertCode(t *testing.T, err error, want contracts.ErrorCode) {
	t.Helper()
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != want {
		t.Fatalf("error = %#v, want %s", err, want)
	}
}
