//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/graphql/generated"
	mcpserver "github.com/aneviaro/stratz-mcp/internal/mcp"
	"github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type seeds struct {
	PlayerID    string
	MatchID     string
	MatchIDs    []string
	LeagueID    string
	HeroID      int64
	HeroStatsTo time.Time
}

type recordingExecutor struct {
	next stratz.Executor
	mu   sync.Mutex
	seen map[string]int
}

func (executor *recordingExecutor) Execute(
	ctx context.Context,
	budget *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	executor.mu.Lock()
	executor.seen[request.OperationName]++
	executor.mu.Unlock()
	return executor.next.Execute(ctx, budget, request)
}

func (executor *recordingExecutor) operations() []string {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	result := make([]string, 0, len(executor.seen))
	for operation := range executor.seen {
		result = append(result, operation)
	}
	sort.Strings(result)
	return result
}

func TestEveryPublicToolAgainstLiveSTRATZ(t *testing.T) {
	envFile := os.Getenv("STRATZ_ENV_FILE")
	if envFile == "" {
		t.Fatal("STRATZ_ENV_FILE is required; run `make test-live` or select an explicit dotenv file")
	}
	loaded, err := config.Load(config.LoadOptions{
		Environ: []string{"STRATZ_ENV_FILE=" + envFile},
		UserCacheDir: func() (string, error) {
			return t.TempDir(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := auth.Load(auth.LoadOptions{
		Environment: loaded.Environment,
		TokenFile:   loaded.TokenFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded.Config.Cache.Enabled = false
	loaded.Config.Cache.Directory = t.TempDir()
	loaded.Config.Features.RuntimeIntrospection = true

	client, err := stratz.New(credential, "live-integration", loaded.Config.Limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := discoverSeeds(ctx, t, client)
	schemaDirectory := filepath.Join(t.TempDir(), "schema")
	document, err := schema.Fetch(ctx, client)
	if err != nil {
		var upstreamErr *stratz.Error
		if errors.As(err, &upstreamErr) {
			t.Fatalf("fetch live schema: %v details=%v", err, upstreamErr.Details)
		}
		t.Fatalf("fetch live schema: %v", err)
	}
	files, manifest, err := schema.Generate(document)
	if err != nil {
		t.Fatalf("generate temporary schema bundle: %v", err)
	}
	if err := schema.WriteBundle(schemaDirectory, files); err != nil {
		t.Fatalf("write temporary schema bundle: %v", err)
	}

	recorder := &recordingExecutor{
		next: client,
		seen: map[string]int{},
	}
	server, err := mcpserver.New(mcpserver.Options{
		Version:         "live-integration",
		SchemaVersion:   manifest.SchemaHash,
		SchemaDirectory: schemaDirectory,
		Config:          loaded.Config,
		Executor:        recorder,
		CursorToken:     credential.Token,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:             time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	mcpClient := sdk.NewClient(
		&sdk.Implementation{Name: "stratz-live-integration", Version: "1"},
		&sdk.ClientOptions{},
	)
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	calledTools := map[string]bool{}
	call := func(t *testing.T, tool string, arguments map[string]any) map[string]any {
		t.Helper()
		calledTools[tool] = true
		if tool != "stratz_server_info" {
			arguments["fresh"] = true
		}
		result, callErr := session.CallTool(ctx, &sdk.CallToolParams{
			Name: tool, Arguments: arguments,
		})
		if callErr != nil {
			t.Fatalf("%s protocol call: %v", tool, callErr)
		}
		if validationErr := contracts.ValidateOutput(tool, result.StructuredContent); validationErr != nil {
			t.Fatalf("%s output contract: %v", tool, validationErr)
		}
		if result.IsError {
			encoded, _ := json.Marshal(result.StructuredContent)
			t.Fatalf("%s returned an error: %s", tool, encoded)
		}
		object, ok := result.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("%s structured content type = %T", tool, result.StructuredContent)
		}
		return object
	}

	probePluralMatchEndpoint(ctx, t, recorder, fixture.MatchIDs)

	t.Run("server_info", func(t *testing.T) {
		output := call(t, "stratz_server_info", map[string]any{})
		data := objectField(t, output, "data")
		if data["mcp_protocol_version"] != contracts.MCPProtocolVersion {
			t.Fatalf("server info protocol version = %#v, want %s", data["mcp_protocol_version"], contracts.MCPProtocolVersion)
		}
		if data["cache_status"] != "disabled" {
			t.Fatalf("server info cache status = %#v, want disabled for live coverage", data["cache_status"])
		}
	})
	t.Run("raw_graphql", func(t *testing.T) {
		output := call(t, "stratz_execute_graphql", map[string]any{
			"query":          `query IntegrationRaw($id: Long!) { match(id: $id) { id } }`,
			"operation_name": "IntegrationRaw",
			"variables":      map[string]any{"id": mustInt64(t, fixture.MatchID)},
		})
		graphql := objectField(t, objectField(t, output, "data"), "graphql")
		if len(arrayField(t, graphql, "errors")) != 0 {
			t.Fatalf("raw GraphQL returned errors: %#v", graphql["errors"])
		}
		match := objectField(t, objectField(t, graphql, "data"), "match")
		if positiveIntField(match, "id") != mustInt64(t, fixture.MatchID) {
			t.Fatalf("raw GraphQL match id = %#v, want %s", match["id"], fixture.MatchID)
		}
	})
	t.Run("players", func(t *testing.T) {
		defaultOutput := call(t, "stratz_query_players", map[string]any{
			"mode": "exact", "player_ids": []any{fixture.PlayerID},
		})
		defaultItems := queryItems(t, defaultOutput, "exact")
		if len(defaultItems) != 1 {
			t.Fatalf("default player item count = %d, want 1", len(defaultItems))
		}
		defaultPlayer := objectValue(t, defaultItems[0])
		if defaultPlayer["account_id"] != fixture.PlayerID {
			t.Fatalf("default player account_id = %#v, want %s", defaultPlayer["account_id"], fixture.PlayerID)
		}
		if _, present := defaultPlayer["statistics"]; present {
			t.Fatalf("default player unexpectedly contains statistics: %#v", defaultPlayer)
		}

		statisticsOutput := call(t, "stratz_query_players", map[string]any{
			"mode": "exact", "player_ids": []any{fixture.PlayerID}, "include_profile_statistics": true,
		})
		statisticsItems := queryItems(t, statisticsOutput, "exact")
		if len(statisticsItems) != 1 {
			t.Fatalf("statistics player item count = %d, want 1", len(statisticsItems))
		}
		statistics := objectValue(t, statisticsItems[0])
		profileStatistics := objectField(t, statistics, "statistics")
		if positiveIntField(profileStatistics, "match_count") <= 0 || positiveIntField(profileStatistics, "win_count") <= 0 {
			t.Fatalf("player statistics are not representative: %#v", profileStatistics)
		}
	})
	t.Run("matches", func(t *testing.T) {
		for _, test := range []struct {
			detail     string
			objectives bool
			timeline   bool
		}{
			{detail: "summary"},
			{detail: "players"},
			{detail: "standard", objectives: true},
			{detail: "full", objectives: true, timeline: true},
		} {
			t.Run(test.detail, func(t *testing.T) {
				output := call(t, "stratz_query_matches", map[string]any{
					"mode": "exact", "match_ids": []any{fixture.MatchID}, "detail_level": test.detail,
				})
				items := queryItems(t, output, "exact")
				if len(items) != 1 {
					t.Fatalf("%s exact item count = %d, want 1", test.detail, len(items))
				}
				match := objectValue(t, items[0])
				if match["match_id"] != fixture.MatchID {
					t.Fatalf("%s match_id = %#v, want %s", test.detail, match["match_id"], fixture.MatchID)
				}
				assertMatchScoreAndPlayers(t, match, test.detail)
				if players, ok := match["players"].([]any); !ok || len(players) == 0 {
					t.Fatalf("%s match has no player rows: %#v", test.detail, match)
				}
				if test.objectives && len(arrayField(t, match, "objectives")) == 0 {
					t.Fatalf("%s match has no objectives: %#v", test.detail, match)
				}
				if test.timeline && len(arrayField(t, match, "timeline")) == 0 {
					t.Fatalf("%s match has no timeline: %#v", test.detail, match)
				}
			})
		}
	})
	t.Run("heroes_and_constants", func(t *testing.T) {
		single := call(t, "stratz_query_constants", map[string]any{
			"mode": "types", "types": []any{"heroes"},
		})
		assertConstantItems(t, single, "types", "heroes")

		multi := call(t, "stratz_query_constants", map[string]any{
			"mode": "types", "types": []any{"heroes", "items"},
		})
		multiItems := queryItems(t, multi, "types")
		if len(multiItems) == 0 {
			t.Fatal("multi-type constants returned no items")
		}
		types := map[string]bool{}
		for _, value := range multiItems {
			item := objectValue(t, value)
			types[stringField(t, item, "type")] = true
		}
		if !types["heroes"] || !types["items"] {
			t.Fatalf("multi-type constants did not preserve type qualification: %v", types)
		}

		typed := call(t, "stratz_query_constants", map[string]any{
			"mode": "typed_selectors",
			"selectors": []any{map[string]any{
				"type": "heroes", "ids": []any{strconv.FormatInt(fixture.HeroID, 10)},
			}},
		})
		typedItems := queryItems(t, typed, "typed_selectors")
		if len(typedItems) != 1 {
			t.Fatalf("typed hero selector item count = %d, want 1", len(typedItems))
		}
		typedItem := objectValue(t, typedItems[0])
		if typedItem["type"] != "heroes" || typedItem["id"] != strconv.FormatInt(fixture.HeroID, 10) {
			t.Fatalf("typed hero selector item = %#v", typedItem)
		}

		defaultHero := call(t, "stratz_query_heroes", map[string]any{
			"mode": "exact", "heroes": []any{fixture.HeroID},
		})
		defaultHeroItems := queryItems(t, defaultHero, "exact")
		if len(defaultHeroItems) != 1 {
			t.Fatalf("default hero item count = %d, want 1", len(defaultHeroItems))
		}
		hero := objectValue(t, defaultHeroItems[0])
		if _, present := hero["statistics"]; present {
			t.Fatalf("default hero unexpectedly contains statistics: %#v", hero)
		}
		if positiveIntField(hero, "hero_id") != fixture.HeroID || stringField(t, hero, "slug") == "" {
			t.Fatalf("default hero has incomplete identity: %#v", hero)
		}

		for _, days := range []int{7, 60, 240} {
			t.Run(fmt.Sprintf("hero_statistics_%dd", days), func(t *testing.T) {
				output := call(t, "stratz_query_heroes", map[string]any{
					"mode": "exact", "heroes": []any{fixture.HeroID}, "include_statistics": true,
					"from": fixture.HeroStatsTo.AddDate(0, 0, -days).Format(time.RFC3339),
					"to":   fixture.HeroStatsTo.Format(time.RFC3339),
				})
				items := queryItems(t, output, "exact")
				if len(items) != 1 {
					t.Fatalf("%d-day hero item count = %d, want 1", days, len(items))
				}
				statistics := objectField(t, objectValue(t, items[0]), "statistics")
				if positiveIntField(statistics, "sample_size") <= 0 {
					t.Fatalf("%d-day hero statistics have no observations: %#v", days, statistics)
				}
				winRate, ok := statistics["win_rate"].(float64)
				if !ok || winRate <= 0 || winRate > 1 {
					t.Fatalf("%d-day hero win_rate = %#v, want representative value in (0,1]", days, statistics["win_rate"])
				}
			})
		}

		search := call(t, "stratz_query_heroes", map[string]any{
			"mode": "search", "query": "a", "limit": 1,
		})
		searchItems := queryItems(t, search, "search")
		if len(searchItems) != 1 {
			t.Fatalf("hero search item count = %d, want 1", len(searchItems))
		}
		if stringField(t, objectValue(t, searchItems[0]), "slug") == "" {
			t.Fatal("hero search returned an item without a slug")
		}
		cursor := requirePageCursor(t, search, "hero search")
		searchNext := call(t, "stratz_query_heroes", map[string]any{
			"mode": "search", "query": "a", "limit": 1, "cursor": cursor,
		})
		if len(queryItems(t, searchNext, "search")) == 0 {
			t.Fatal("hero search continuation returned no item")
		}
	})
	t.Run("leagues", func(t *testing.T) {
		exact := call(t, "stratz_query_leagues", map[string]any{
			"mode": "exact", "league_ids": []any{fixture.LeagueID},
		})
		exactItems := queryItems(t, exact, "exact")
		if len(exactItems) != 1 {
			t.Fatalf("exact league item count = %d, want 1", len(exactItems))
		}
		league := objectValue(t, exactItems[0])
		if league["league_id"] != fixture.LeagueID || stringField(t, league, "name") == "" {
			t.Fatalf("exact league has incomplete identity: %#v", league)
		}

		search := call(t, "stratz_query_leagues", map[string]any{
			"mode": "search", "limit": 1,
		})
		searchItems := queryItems(t, search, "search")
		if len(searchItems) != 1 {
			t.Fatalf("league search item count = %d, want 1", len(searchItems))
		}
		if stringField(t, objectValue(t, searchItems[0]), "name") == "" {
			t.Fatal("league search returned an item without a name")
		}
		cursor := requirePageCursor(t, search, "league search")
		searchNext := call(t, "stratz_query_leagues", map[string]any{
			"mode": "search", "limit": 1, "cursor": cursor,
		})
		if len(queryItems(t, searchNext, "search")) == 0 {
			t.Fatal("league search continuation returned no item")
		}
	})
	t.Run("match_history_and_live", func(t *testing.T) {
		for _, detail := range []string{"summary", "players"} {
			t.Run("player_history_"+detail, func(t *testing.T) {
				arguments := map[string]any{
					"mode": "player_history", "player_id": fixture.PlayerID, "limit": 1,
					"detail_level": detail,
				}
				output := call(t, "stratz_query_matches", arguments)
				items := queryItems(t, output, "player_history")
				if len(items) == 0 {
					t.Fatal("player history returned no matches")
				}
				match := objectValue(t, items[0])
				assertMatchScoreAndPlayers(t, match, "player_history_"+detail)
				if detail == "players" {
					player := objectField(t, match, "player")
					if player["account_id"] != fixture.PlayerID {
						t.Fatalf("player history row account_id = %#v, want %s", player["account_id"], fixture.PlayerID)
					}
				}
				cursor := requirePageCursor(t, output, "player history "+detail)
				continuation := call(t, "stratz_query_matches", map[string]any{
					"mode": "player_history", "player_id": fixture.PlayerID, "limit": 1,
					"detail_level": detail, "cursor": cursor,
				})
				queryItems(t, continuation, "player_history")
				pageObject(t, continuation, "player history continuation "+detail)
			})
		}

		leagueHistory := call(t, "stratz_query_matches", map[string]any{
			"mode": "league_history", "league_id": fixture.LeagueID, "limit": 1,
			"detail_level": "summary",
		})
		leagueItems := queryItems(t, leagueHistory, "league_history")
		if len(leagueItems) == 0 {
			t.Fatal("league history returned no matches")
		}
		assertMatchScoreAndPlayers(t, objectValue(t, leagueItems[0]), "league_history")
		leagueCursor := requirePageCursor(t, leagueHistory, "league history")
		leagueContinuation := call(t, "stratz_query_matches", map[string]any{
			"mode": "league_history", "league_id": fixture.LeagueID, "limit": 1,
			"detail_level": "summary", "cursor": leagueCursor,
		})
		queryItems(t, leagueContinuation, "league_history")
		pageObject(t, leagueContinuation, "league history continuation")
		live := call(t, "stratz_query_matches", map[string]any{
			"mode": "live", "limit": 1, "sort": "highest_profile",
		})
		liveItems := queryItems(t, live, "live")
		for _, value := range liveItems {
			match := objectValue(t, value)
			if stringField(t, match, "match_id") == "" {
				t.Fatalf("live match has no match_id: %#v", match)
			}
			players, ok := match["players"].([]any)
			if !ok {
				t.Fatalf("live match players type = %T, want array", match["players"])
			}
			for _, playerValue := range players {
				player := objectValue(t, playerValue)
				team := stringField(t, player, "team")
				if team != "radiant" && team != "dire" {
					t.Fatalf("live player team = %q", team)
				}
			}
		}
		if page := pageObject(t, live, "live matches"); page["has_more"] == true {
			cursor, _ := page["next_cursor"].(string)
			continuation := call(t, "stratz_query_matches", map[string]any{
				"mode": "live", "limit": 1, "sort": "highest_profile", "cursor": cursor,
			})
			queryItems(t, continuation, "live")
		}
	})

	wantOperations := []string{
		"IntegrationRaw",
		"StratzGetConstants",
		"StratzGetHeroStatsDay",
		"StratzGetHeroStatsMonth",
		"StratzGetHeroStatsWeek",
		"StratzGetMatchBatchFull",
		"StratzGetMatchBatchStandard",
		"StratzGetMatchBatchSummary",
		"StratzGetMatchesSummary",
		"StratzGetPlayers",
		"StratzGetPlayersLean",
		"StratzListLeagueMatches",
		"StratzListLeagues",
		"StratzListLiveMatches",
		"StratzListPlayerMatches",
		"StratzListPlayerMatchesWithPlayers",
		"StratzMCPHealth",
	}
	if got := recorder.operations(); !reflect.DeepEqual(got, wantOperations) {
		t.Fatalf("live operation coverage\n got: %v\nwant: %v", got, wantOperations)
	}
	wantTools := make([]string, 0, len(contracts.Definitions()))
	for _, definition := range contracts.Definitions() {
		wantTools = append(wantTools, definition.Name)
	}
	sort.Strings(wantTools)
	gotTools := make([]string, 0, len(calledTools))
	for tool := range calledTools {
		gotTools = append(gotTools, tool)
	}
	sort.Strings(gotTools)
	if !reflect.DeepEqual(gotTools, wantTools) {
		t.Fatalf("live tool coverage\n got: %v\nwant: %v", gotTools, wantTools)
	}
}

func discoverSeeds(ctx context.Context, t *testing.T, executor stratz.Executor) seeds {
	t.Helper()
	const (
		heroID            = int64(1)
		maxPlaybackProbes = 25
	)
	playerID := os.Getenv("STRATZ_TEST_PLAYER_ID")
	if playerID == "" {
		playerID = "169047571"
	}
	if _, err := strconv.ParseUint(playerID, 10, 32); err != nil {
		t.Fatalf("STRATZ_TEST_PLAYER_ID is invalid: %v", err)
	}
	heroStatsTo := discoverLatestHeroStatsTo(ctx, t, executor, heroID)

	leagueData := execute(ctx, t, executor, "StratzIntegrationLeagueSeeds", `
		query StratzIntegrationLeagueSeeds($request: LeagueRequestType!) {
			leagues(request: $request) { id }
		}
	`, map[string]any{"request": map[string]any{
		"leagueEnded": true, "orderBy": "LAST_MATCH_TIME", "take": 25, "skip": 0,
	}})
	var leagueEnvelope struct {
		Leagues []struct {
			ID int64 `json:"id"`
		} `json:"leagues"`
	}
	if err := json.Unmarshal(leagueData, &leagueEnvelope); err != nil || len(leagueEnvelope.Leagues) == 0 {
		t.Fatalf("discover league seeds: %v", err)
	}

	candidates := make([]seeds, 0, 2)
	seenMatches := make(map[int64]struct{})
	playbackProbes := 0
leagueLoop:
	for _, league := range leagueEnvelope.Leagues {
		matchData := execute(ctx, t, executor, "StratzIntegrationMatchSeeds", `
			query StratzIntegrationMatchSeeds($id: Int!, $request: LeagueMatchesRequestType!) {
				league(id: $id) {
					matches(request: $request) {
						id
						parsedDateTime
						statsDateTime
						radiantKills
						direKills
					}
				}
			}
		`, map[string]any{
			"id": league.ID, "request": map[string]any{"take": 10, "skip": 0},
		})
		var matchEnvelope struct {
			League *struct {
				Matches []struct {
					ID             int64           `json:"id"`
					ParsedDateTime *int64          `json:"parsedDateTime"`
					StatsDateTime  *int64          `json:"statsDateTime"`
					RadiantKills   json.RawMessage `json:"radiantKills"`
					DireKills      json.RawMessage `json:"direKills"`
				} `json:"matches"`
			} `json:"league"`
		}
		if json.Unmarshal(matchData, &matchEnvelope) != nil || matchEnvelope.League == nil {
			continue
		}
		for _, match := range matchEnvelope.League.Matches {
			if match.ParsedDateTime == nil || match.StatsDateTime == nil ||
				killCount(match.RadiantKills)+killCount(match.DireKills) == 0 {
				continue
			}
			if _, duplicate := seenMatches[match.ID]; duplicate {
				continue
			}
			seenMatches[match.ID] = struct{}{}
			if playbackProbes == maxPlaybackProbes {
				break leagueLoop
			}
			playbackProbes++

			playbackData := execute(ctx, t, executor, "StratzIntegrationMatchPlaybackSeed", `
				query StratzIntegrationMatchPlaybackSeed($id: Long!) {
					match(id: $id) {
						id
						playbackData {
							buildingEvents { time }
							roshanEvents { time }
							towerDeathEvents { time }
							runeEvents { time }
							wardEvents { time }
						}
					}
				}
			`, map[string]any{"id": match.ID})
			var playbackEnvelope struct {
				Match *struct {
					PlaybackData *struct {
						BuildingEvents   []json.RawMessage `json:"buildingEvents"`
						RoshanEvents     []json.RawMessage `json:"roshanEvents"`
						TowerDeathEvents []json.RawMessage `json:"towerDeathEvents"`
						RuneEvents       []json.RawMessage `json:"runeEvents"`
						WardEvents       []json.RawMessage `json:"wardEvents"`
					} `json:"playbackData"`
				} `json:"match"`
			}
			if json.Unmarshal(playbackData, &playbackEnvelope) != nil ||
				playbackEnvelope.Match == nil || playbackEnvelope.Match.PlaybackData == nil {
				continue
			}
			playback := playbackEnvelope.Match.PlaybackData
			objectiveCount := len(playback.BuildingEvents) +
				len(playback.RoshanEvents) +
				len(playback.TowerDeathEvents)
			timelineCount := len(playback.RuneEvents) + len(playback.WardEvents)
			if objectiveCount == 0 || timelineCount == 0 {
				continue
			}

			candidates = append(candidates, seeds{
				MatchID:  strconv.FormatInt(match.ID, 10),
				LeagueID: strconv.FormatInt(league.ID, 10),
			})
			if len(candidates) == 2 {
				return seeds{
					PlayerID:    playerID,
					MatchID:     candidates[0].MatchID,
					MatchIDs:    []string{candidates[0].MatchID, candidates[1].MatchID},
					LeagueID:    candidates[0].LeagueID,
					HeroID:      heroID,
					HeroStatsTo: heroStatsTo,
				}
			}
		}
	}
	t.Fatalf(
		"fewer than two parsed league matches with scores, objectives, and timeline events were available after %d playback probes",
		playbackProbes,
	)
	return seeds{}
}

func discoverLatestHeroStatsTo(
	ctx context.Context,
	t *testing.T,
	executor stratz.Executor,
	heroID int64,
) time.Time {
	t.Helper()
	data := execute(ctx, t, executor, "StratzIntegrationHeroStatsSeed", `
		query StratzIntegrationHeroStatsSeed($heroIds: [Short!]) {
			heroStats {
				stats: winDay(heroIds: $heroIds, take: 400, skip: 0, groupBy: TIME) {
					matchCount
					period: day
				}
			}
		}
	`, map[string]any{"heroIds": []int64{heroID}})
	var envelope struct {
		HeroStats *struct {
			Stats []struct {
				MatchCount int64 `json:"matchCount"`
				Period     int64 `json:"period"`
			} `json:"stats"`
		} `json:"heroStats"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.HeroStats == nil {
		t.Fatalf("discover latest hero-stat period for hero %d: %v", heroID, err)
	}
	var latestPeriod int64
	for _, row := range envelope.HeroStats.Stats {
		if row.MatchCount > 0 && row.Period > latestPeriod {
			latestPeriod = row.Period
		}
	}
	if latestPeriod == 0 {
		t.Fatalf("discover latest hero-stat period for hero %d: no populated winDay rows", heroID)
	}
	latestDay := time.Unix(latestPeriod, 0).UTC()
	return time.Date(latestDay.Year(), latestDay.Month(), latestDay.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
}

func probePluralMatchEndpoint(
	ctx context.Context,
	t *testing.T,
	executor stratz.Executor,
	matchIDs []string,
) {
	t.Helper()
	budget, err := stratz.NewRequestBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{mustInt64(t, matchIDs[0]), mustInt64(t, matchIDs[1])}
	response, err := executor.Execute(ctx, budget, stratz.Request{
		Query:         generated.StratzGetMatchesSummary_Operation,
		OperationName: "StratzGetMatchesSummary",
		Variables:     map[string]any{"ids": ids},
		Mode:          stratz.ModeCurated,
		AllowRetries:  true,
	})
	if err != nil {
		var upstreamErr *stratz.Error
		if !errors.As(err, &upstreamErr) ||
			upstreamErr.Code != contracts.ErrorCodeUpstreamPartialError ||
			!strings.Contains(fmt.Sprint(upstreamErr.Details), "User is not an admin.") {
			t.Fatalf("plural match capability probe: %v", err)
		}
		return
	}
	var envelope struct {
		Matches []struct {
			ID int64 `json:"id"`
		} `json:"matches"`
	}
	if response == nil || json.Unmarshal(response.Data, &envelope) != nil || len(envelope.Matches) != 2 {
		t.Fatalf("plural match capability probe returned invalid data: %#v", response)
	}
}

func killCount(data json.RawMessage) int64 {
	var count int64
	if json.Unmarshal(data, &count) == nil {
		return count
	}
	var events []json.RawMessage
	if json.Unmarshal(data, &events) == nil {
		return int64(len(events))
	}
	return 0
}

func objectField(t *testing.T, object map[string]any, field string) map[string]any {
	t.Helper()
	return objectValue(t, object[field])
}

func objectValue(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value type = %T, want object", value)
	}
	return object
}

func arrayField(t *testing.T, object map[string]any, field string) []any {
	t.Helper()
	items, ok := object[field].([]any)
	if !ok {
		t.Fatalf("%s type = %T, want array", field, object[field])
	}
	return items
}

func positiveIntField(object map[string]any, field string) int64 {
	switch value := object[field].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		number, _ := value.Int64()
		return number
	default:
		return 0
	}
}

func queryItems(t *testing.T, output map[string]any, mode string) []any {
	t.Helper()
	data := objectField(t, output, "data")
	if got := data["mode"]; got != mode {
		t.Fatalf("query mode = %#v, want %s", got, mode)
	}
	return arrayField(t, data, "items")
}

func pageObject(t *testing.T, output map[string]any, label string) map[string]any {
	t.Helper()
	data := objectField(t, output, "data")
	page := objectField(t, data, "page")
	hasMore, ok := page["has_more"].(bool)
	if !ok {
		t.Fatalf("%s has_more type = %T, want bool", label, page["has_more"])
	}
	if cursor, present := page["next_cursor"]; present && cursor != nil {
		if _, ok := cursor.(string); !ok {
			t.Fatalf("%s next_cursor type = %T, want string or null", label, cursor)
		}
	}
	if hasMore {
		cursor, _ := page["next_cursor"].(string)
		if cursor == "" {
			t.Fatalf("%s advertises more results without a cursor: %#v", label, page)
		}
	}
	return page
}

func requirePageCursor(t *testing.T, output map[string]any, label string) string {
	t.Helper()
	page := pageObject(t, output, label)
	if page["has_more"] != true {
		t.Fatalf("%s has_more = %#v, want true for pagination coverage", label, page["has_more"])
	}
	cursor, ok := page["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("%s next_cursor = %#v, want non-empty string", label, page["next_cursor"])
	}
	return cursor
}

func assertConstantItems(t *testing.T, output map[string]any, mode string, wantType string) {
	t.Helper()
	items := queryItems(t, output, mode)
	if len(items) == 0 {
		t.Fatalf("%s constants returned no items", mode)
	}
	for _, value := range items {
		item := objectValue(t, value)
		if got := stringField(t, item, "type"); got != wantType {
			t.Fatalf("constant type = %q, want %s", got, wantType)
		}
	}
}

func assertMatchScoreAndPlayers(t *testing.T, match map[string]any, label string) {
	t.Helper()
	if positiveIntField(match, "radiant_score")+positiveIntField(match, "dire_score") <= 0 {
		t.Fatalf("%s match has no observed score: %#v", label, match)
	}
	if players, present := match["players"]; present {
		rows, ok := players.([]any)
		if !ok || len(rows) == 0 {
			t.Fatalf("%s match has no player rows: %#v", label, match)
		}
	}
}

func stringField(t *testing.T, object map[string]any, field string) string {
	t.Helper()
	value, ok := object[field].(string)
	if !ok {
		t.Fatalf("%s type = %T, want string", field, object[field])
	}
	return value
}

func execute(
	ctx context.Context,
	t *testing.T,
	executor stratz.Executor,
	operation string,
	query string,
	variables map[string]any,
) json.RawMessage {
	t.Helper()
	budget, err := stratz.NewRequestBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	response, err := executor.Execute(ctx, budget, stratz.Request{
		Query: query, OperationName: operation, Variables: variables,
		Mode: stratz.ModeCurated, AllowRetries: true,
	})
	if err != nil {
		t.Fatalf("%s: %v", operation, err)
	}
	if response == nil {
		t.Fatalf("%s returned no response", operation)
	}
	return response.Data
}

func mustInt64(t *testing.T, value string) int64 {
	t.Helper()
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatal(fmt.Errorf("parse %q: %w", value, err))
	}
	return number
}
