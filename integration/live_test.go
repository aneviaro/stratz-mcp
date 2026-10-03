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
		call(t, "stratz_server_info", map[string]any{})
	})
	t.Run("raw_graphql", func(t *testing.T) {
		call(t, "stratz_execute_graphql", map[string]any{
			"query":          `query IntegrationRaw($id: Long!) { match(id: $id) { id } }`,
			"operation_name": "IntegrationRaw",
			"variables":      map[string]any{"id": mustInt64(t, fixture.MatchID)},
		})
	})
	t.Run("players", func(t *testing.T) {
		call(t, "stratz_get_player", map[string]any{"player_id": fixture.PlayerID, "detail_level": "summary"})
		call(t, "stratz_batch_get_players", map[string]any{"player_ids": []any{fixture.PlayerID}, "detail_level": "summary"})
		call(t, "stratz_list_player_matches", map[string]any{"player_id": fixture.PlayerID, "limit": 1})
	})
	t.Run("matches", func(t *testing.T) {
		for _, detail := range []string{"summary", "standard", "full"} {
			output := call(t, "stratz_get_match", map[string]any{"match_id": fixture.MatchID, "detail_level": detail})
			match := objectField(t, output, "data")
			if positiveIntField(match, "radiant_score")+positiveIntField(match, "dire_score") == 0 {
				t.Fatalf("%s match has no observed score: %#v", detail, match)
			}
			if detail != "summary" && len(arrayField(t, match, "objectives")) == 0 {
				t.Fatalf("%s match has no objectives: %#v", detail, match)
			}
			if detail == "full" {
				if len(arrayField(t, match, "timeline")) == 0 {
					t.Fatalf("full match has no timeline: %#v", match)
				}
				if len(arrayField(t, output, "warnings")) == 0 {
					t.Fatalf("full match did not report unavailable detail fields: %#v", output)
				}
			}
		}
		for _, detail := range []string{"summary", "standard", "full"} {
			t.Run("batch_"+detail, func(t *testing.T) {
				matchIDs := []any{fixture.MatchID, fixture.MatchID}
				output := call(t, "stratz_batch_get_matches", map[string]any{
					"match_ids": matchIDs, "detail_level": detail,
				})
				items := arrayField(t, objectField(t, output, "data"), "items")
				if len(items) != 2 {
					t.Fatalf("%s batch item count = %d, want 2", detail, len(items))
				}
				first := objectValue(t, items[0])
				second := objectValue(t, items[1])
				if first["match_id"] != second["match_id"] {
					t.Fatalf("%s batch did not reconstruct duplicate input: %#v", detail, items)
				}
			})
		}
	})
	t.Run("heroes_and_constants", func(t *testing.T) {
		constants := call(t, "stratz_get_constants", map[string]any{"type": "heroes"})
		if len(arrayField(t, objectField(t, constants, "data"), "items")) == 0 {
			t.Fatal("hero constants returned no items")
		}
		call(t, "stratz_get_hero", map[string]any{"hero": fixture.HeroID, "detail_level": "summary"})
		call(t, "stratz_batch_get_heroes", map[string]any{"heroes": []any{fixture.HeroID}, "detail_level": "summary"})
		for _, days := range []int{7, 60, 240} {
			output := call(t, "stratz_get_hero_stats", map[string]any{
				"hero": fixture.HeroID,
				"from": fixture.HeroStatsTo.AddDate(0, 0, -days).Format(time.RFC3339),
				"to":   fixture.HeroStatsTo.Format(time.RFC3339),
			})
			stats := objectField(t, output, "data")
			if positiveIntField(stats, "sample_size") == 0 || stats["win_rate"] == nil {
				t.Fatalf("%d-day hero statistics lack observed wins: %#v", days, stats)
			}
		}
	})
	t.Run("leagues_and_live", func(t *testing.T) {
		call(t, "stratz_list_leagues", map[string]any{"limit": 1})
		call(t, "stratz_get_league", map[string]any{"league_id": fixture.LeagueID, "detail_level": "summary"})
		call(t, "stratz_list_league_matches", map[string]any{"league_id": fixture.LeagueID, "limit": 1})
		call(t, "stratz_list_live_matches", map[string]any{"limit": 1, "sort": "highest_profile"})
	})

	wantOperations := []string{
		"IntegrationRaw",
		"StratzGetConstants",
		"StratzGetHeroStatsDay",
		"StratzGetHeroStatsMonth",
		"StratzGetHeroStatsWeek",
		"StratzGetLeague",
		"StratzGetMatchBatchFull",
		"StratzGetMatchBatchStandard",
		"StratzGetMatchBatchSummary",
		"StratzGetMatchFull",
		"StratzGetMatchStandard",
		"StratzGetMatchSummary",
		"StratzGetMatchesSummary",
		"StratzGetPlayer",
		"StratzGetPlayers",
		"StratzListLeagueMatches",
		"StratzListLeagues",
		"StratzListLiveMatches",
		"StratzListPlayerMatches",
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
