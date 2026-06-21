package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
	"github.com/aneviaro/stratz-mcp/internal/domain/leaguelive"
)

func TestLeagueLiveEnvelopesValidate(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	options := Options{
		Version: "test", SchemaVersion: "sha256:fixture", Config: config.Defaults(t.TempDir()),
		Now: func() time.Time { return now },
	}
	league := contracts.League{LeagueID: "1", Name: "League", Status: stringPointer("live")}
	if _, err := SuccessResult("stratz_get_league", leagueLiveEnvelope(
		options, "get_league", contracts.DetailLevelStandard,
		&leaguelive.Result[contracts.League]{Data: league}, false, nil,
	)); err != nil {
		t.Fatal(err)
	}
	live := contracts.StratzListLiveMatchesData{
		Items: []contracts.LiveMatch{}, Page: contracts.PageInfo{HasMore: true, NextCursor: stringPointer("cursor")},
	}
	if _, err := SuccessResult("stratz_list_live_matches", leagueLiveEnvelope(
		options, "list_live_matches", "",
		&leaguelive.Result[contracts.StratzListLiveMatchesData]{
			Data: live, Warnings: []string{"not a snapshot"},
		}, false, nil,
	)); err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string { return &value }

func TestHeroFilteredLiveMatchListSharesRequestBudget(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	executor := &budgetFixtureExecutor{}
	heroes, err := heroconstants.New(heroconstants.Options{
		Executor: executor, MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
	})
	if err != nil {
		t.Fatal(err)
	}
	live, err := leaguelive.New(leaguelive.Options{
		Executor: executor, Token: "token", SchemaVersion: "schema-v1",
		MaxUpstreamRequests: cfg.Limits.MaxUpstreamRequests,
	})
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Config: cfg, SchemaVersion: "schema-v1", Now: time.Now}
	handlers := map[string]ToolHandler{}
	registerLeagueLiveHandlers(handlers, options, live, heroes)

	_, err = handlers["stratz_list_live_matches"](context.Background(), map[string]any{
		"hero":               "axe",
		"limit":              json.Number("1"),
		"minimum_spectators": json.Number("999"),
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
