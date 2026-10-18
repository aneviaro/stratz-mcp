package mcp

import (
	"encoding/json"
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

	heroID := int64(7)
	partyID := int64(42)
	networth := int64(21000)
	level := int64(25)
	imp := 9.25
	won := true
	matchData := playermatch.MatchQueryData{
		Mode: "exact",
		Items: []contracts.Match{{MatchID: "1", ParseStatus: "parsed", Players: []contracts.MatchPlayer{
			{
				AccountID: stringPointer("1"), HeroID: &heroID, Team: "radiant", PlayerSlot: 4,
				Position: stringPointer("POSITION_1"), Role: stringPointer("CORE"),
				RoleBasic: stringPointer("LIGHT_SUPPORT"), Lane: stringPointer("SAFE_LANE"), PartyID: &partyID,
				Kills: 11, Deaths: 1, Assists: 14, Networth: &networth, Level: &level, Won: &won, Imp: &imp,
			},
			{
				AccountID: nil, HeroID: nil, Team: "dire", PlayerSlot: 8,
				Position: nil, Role: stringPointer("UNKNOWN"), RoleBasic: stringPointer("UNKNOWN"),
				Lane: stringPointer("UNKNOWN"), PartyID: nil, Kills: 1, Deaths: 2, Assists: 3,
			},
		}}},
	}
	result, err := SuccessResult("stratz_query_matches", queryMatchesEnvelope(
		options, matchData, nil, nil, nil, false, contracts.DetailLevelStandard,
	))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := result.StructuredContent.(json.RawMessage)
	if !ok {
		t.Fatalf("MCP structured content type = %T, want JSON object", result.StructuredContent)
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatal(err)
	}
	data, ok := encoded["data"].(map[string]any)
	if !ok {
		t.Fatalf("MCP match data = %#v", encoded["data"])
	}
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("MCP match items = %#v", data["items"])
	}
	match, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("MCP match item = %#v", items[0])
	}
	players, ok := match["players"].([]any)
	if !ok || len(players) != 2 {
		t.Fatalf("MCP match players = %#v", match["players"])
	}
	for index, value := range players {
		player, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("MCP player[%d] = %#v", index, value)
		}
		for _, field := range []string{"player_slot", "position", "role", "role_basic", "lane", "party_id"} {
			if _, present := player[field]; !present {
				t.Fatalf("MCP player[%d] missing %q: %#v", index, field, player)
			}
		}
		if index == 0 {
			if player["player_slot"] != float64(4) || player["position"] != "POSITION_1" ||
				player["role"] != "CORE" || player["role_basic"] != "LIGHT_SUPPORT" ||
				player["lane"] != "SAFE_LANE" || player["party_id"] != float64(42) ||
				player["kills"] != float64(11) || player["deaths"] != float64(1) || player["assists"] != float64(14) ||
				player["team"] != "radiant" || player["won"] != true {
				t.Fatalf("MCP populated player = %#v, want representative metadata and KDA", player)
			}
		}
		if index == 1 {
			if player["position"] != nil || player["role"] != "UNKNOWN" || player["role_basic"] != "UNKNOWN" ||
				player["lane"] != "UNKNOWN" || player["party_id"] != nil {
				t.Fatalf("MCP null/unknown player = %#v", player)
			}
		}
	}
}

func stringPointer(value string) *string { return &value }

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
