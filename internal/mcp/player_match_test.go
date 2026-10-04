package mcp

import (
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/playermatch"
)

func TestQueryPlayerAndMatchEnvelopesValidate(t *testing.T) {
	options := Options{
		Version: "test", SchemaVersion: "sha256:fixture", Config: config.Defaults(t.TempDir()),
		Now: func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) },
	}
	playerData := map[string]any{
		"mode":  "exact",
		"items": []contracts.Player{{AccountID: "1", IsPrivate: false}},
	}
	if _, err := SuccessResult("stratz_query_players", curatedEnvelope(
		options, "query_players", "", playerData, nil, false, nil, nil,
	)); err != nil {
		t.Fatal(err)
	}

	matchData := playermatch.MatchQueryData{
		Mode:  "exact",
		Items: []contracts.Match{{MatchID: "1", ParseStatus: "parsed", Players: []contracts.MatchPlayer{}}},
	}
	if _, err := SuccessResult("stratz_query_matches", queryMatchesEnvelope(
		options, matchData, nil, nil, nil, false, contracts.DetailLevelStandard,
	)); err != nil {
		t.Fatal(err)
	}
}

func TestDetailAuthorizationIsModeAware(t *testing.T) {
	for _, detail := range []string{"players", "standard", "full"} {
		if detailAllowedForRequest("stratz_query_matches", map[string]any{
			"mode": "league_history", "detail_level": detail,
		}) {
			t.Fatalf("league history accepted %s detail", detail)
		}
	}
	if detailAllowedForRequest("stratz_query_matches", map[string]any{
		"mode": "live", "detail_level": "players",
	}) {
		t.Fatal("live mode accepted player detail")
	}
	if !detailAllowedForRequest("stratz_query_matches", map[string]any{
		"mode": "player_history", "detail_level": "players",
	}) {
		t.Fatal("player history rejected player detail")
	}
	if detailAllowedForRequest("stratz_query_matches", map[string]any{
		"mode": "player_history", "detail_level": "standard",
	}) {
		t.Fatal("player history accepted standard detail")
	}
}

func TestMatchDetailInputReportsEffectiveModeDefaults(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  contracts.DetailLevel
	}{
		{name: "exact default", input: map[string]any{"mode": "exact"}, want: contracts.DetailLevelStandard},
		{name: "player history default", input: map[string]any{"mode": "player_history"}, want: contracts.DetailLevelSummary},
		{name: "player history players", input: map[string]any{"mode": "player_history", "detail_level": "players"}, want: contracts.DetailLevel("players")},
		{name: "league history default", input: map[string]any{"mode": "league_history"}, want: contracts.DetailLevelSummary},
		{name: "live has no detail", input: map[string]any{"mode": "live"}, want: ""},
		{name: "live ignores invalid detail", input: map[string]any{"mode": "live", "detail_level": "full"}, want: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := matchDetailInput(test.input); got != test.want {
				t.Fatalf("matchDetailInput() = %q, want %q", got, test.want)
			}
		})
	}
}
