package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
	"github.com/aneviaro/stratz-mcp/internal/domain/playermatch"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

func TestCuratedPlayerAndMatchEnvelopesValidate(t *testing.T) {
	options := Options{
		Version:       "test",
		SchemaVersion: "sha256:fixture",
		Config:        config.Defaults(t.TempDir()),
		Now: func() time.Time {
			return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
		},
	}
	player := contracts.Player{
		AccountID: "1",
		IsPrivate: false,
	}
	playerEnvelope := curatedEnvelope(
		options,
		"get_player",
		contracts.DetailLevelSummary,
		player,
		map[string]any{"player": map[string]any{"steamAccountId": 1}},
		true,
		nil,
		nil,
	)
	if _, err := SuccessResult("stratz_get_player", playerEnvelope); err != nil {
		t.Fatal(err)
	}

	match := contracts.Match{
		MatchID:     "1",
		ParseStatus: "parsed",
		Players:     []contracts.MatchPlayer{},
	}
	matchEnvelope := curatedEnvelope(
		options,
		"get_match",
		contracts.DetailLevelSummary,
		match,
		nil,
		false,
		nil,
		nil,
	)
	if _, err := SuccessResult("stratz_get_match", matchEnvelope); err != nil {
		t.Fatal(err)
	}
}

func TestDataNotReadyErrorEnvelopeValidates(t *testing.T) {
	err := &ExecutionError{
		Code:      contracts.ErrorCodeDataNotReady,
		Message:   "not ready",
		Details:   map[string]any{"parse_status": "pending"},
		Retryable: false,
		Context: contracts.MatchAvailabilityContext{
			Type: "match_availability",
			Match: contracts.MatchSummary{
				MatchID:     "1",
				ParseStatus: "pending",
			},
			RequestedDetailLevel: contracts.DetailLevelFull,
		},
	}
	result, encodeErr := ErrorResult("stratz_get_match", err)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if !result.IsError {
		t.Fatal("DATA_NOT_READY did not use the MCP error path")
	}
}

func TestNonMatchPlayerToolRejectsPlayersDetail(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	executor := &budgetFixtureExecutor{}
	heroes, err := heroconstants.New(heroconstants.Options{
		Executor: executor, MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := playermatch.New(playermatch.Options{
		Executor: executor, Token: "token", SchemaVersion: "schema-v1",
		MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
		MaxBatchSize:        cfg.Limits.MaxBatchSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	handlers := map[string]ToolHandler{}
	registerPlayerMatchHandlers(handlers, Options{Config: cfg, SchemaVersion: "schema-v1", Now: time.Now}, matches, heroes)

	_, err = handlers["stratz_get_player"](context.Background(), map[string]any{
		"player_id":    "1",
		"detail_level": "players",
	})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != contracts.ErrorCodeInvalidArgument {
		t.Fatalf("error = %#v, want INVALID_ARGUMENT", err)
	}
}

func TestPlayerMatchListPlayersDetailIncludesPlayerRows(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	executor := &budgetFixtureExecutor{}
	heroes, err := heroconstants.New(heroconstants.Options{
		Executor: executor, MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := playermatch.New(playermatch.Options{
		Executor: executor, Token: "token", SchemaVersion: "schema-v1",
		MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
		MaxBatchSize:        cfg.Limits.MaxBatchSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	handlers := map[string]ToolHandler{}
	registerPlayerMatchHandlers(handlers, Options{Config: cfg, SchemaVersion: "schema-v1", Now: time.Now}, matches, heroes)

	output, err := handlers["stratz_list_player_matches"](context.Background(), map[string]any{
		"player_id":      "1",
		"limit":          json.Number("1"),
		"detail_level":   "players",
		"include_player": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	data := output.(map[string]any)["data"].(contracts.StratzListPlayerMatchesData)
	if len(data.Items) != 1 || data.Items[0].Player == nil {
		t.Fatalf("items = %#v, want requested player row", data.Items)
	}
}

func TestHeroFilteredPlayerMatchListSharesRequestBudget(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	executor := &budgetFixtureExecutor{}
	heroes, err := heroconstants.New(heroconstants.Options{
		Executor: executor, MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := playermatch.New(playermatch.Options{
		Executor: executor, Token: "token", SchemaVersion: "schema-v1",
		MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
		MaxBatchSize:        cfg.Limits.MaxBatchSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Config: cfg, SchemaVersion: "schema-v1", Now: time.Now}
	handlers := map[string]ToolHandler{}
	registerPlayerMatchHandlers(handlers, options, matches, heroes)

	_, err = handlers["stratz_list_player_matches"](context.Background(), map[string]any{
		"player_id":                "1",
		"hero":                     "axe",
		"limit":                    json.Number("1"),
		"minimum_duration_seconds": json.Number("999"),
	})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) ||
		executionErr.Code != contracts.ErrorCodeRequestBudgetExceeded {
		t.Fatalf("error = %#v, want REQUEST_BUDGET_EXCEEDED", err)
	}
	if executor.attempts != cfg.Limits.MaxUpstreamRequests {
		t.Fatalf("upstream attempts = %d, want %d", executor.attempts, cfg.Limits.MaxUpstreamRequests)
	}
}

type budgetFixtureExecutor struct {
	attempts int
}

func (executor *budgetFixtureExecutor) Execute(
	_ context.Context,
	budget *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	if !budget.Take() {
		return nil, &stratz.Error{
			Code: contracts.ErrorCodeRequestBudgetExceeded, Message: "budget exhausted",
			Details: map[string]any{},
		}
	}
	executor.attempts++
	switch request.OperationName {
	case "StratzGetConstants":
		return &stratz.Response{Data: json.RawMessage(
			`{"constants":{"heroes":[{"id":1,"name":"npc_dota_hero_axe","localizedName":"Axe","roles":[]}]}}`,
		)}, nil
	case "StratzListPlayerMatches", "StratzListPlayerMatchesWithPlayers":
		items := make([]string, 20)
		for index := range items {
			items[index] = fmt.Sprintf(
				`{"id":%d,"durationSeconds":1,"parseStatus":"parsed","players":[{"steamAccountId":1,"heroId":1,"isRadiant":true,"playerSlot":0,"kills":1,"deaths":2,"assists":3}]}`,
				executor.attempts*100+index,
			)
		}
		return &stratz.Response{Data: json.RawMessage(
			`{"player":{"steamAccountId":1,"matches":[` + strings.Join(items, ",") + `]}}`,
		)}, nil
	case "StratzListLiveMatches":
		items := make([]string, 20)
		for index := range items {
			items[index] = fmt.Sprintf(
				`{"id":%d,"spectatorCount":1,"players":[]}`,
				executor.attempts*100+index,
			)
		}
		return &stratz.Response{Data: json.RawMessage(
			`{"live":{"matches":[` + strings.Join(items, ",") + `]}}`,
		)}, nil
	default:
		return nil, fmt.Errorf("unexpected operation %s", request.OperationName)
	}
}
