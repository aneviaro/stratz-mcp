package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedContractsAreCompleteAndValidateExamples(t *testing.T) {
	definitions := Definitions()
	wantNames := []string{
		"stratz_execute_graphql",
		"stratz_query_constants",
		"stratz_query_heroes",
		"stratz_query_leagues",
		"stratz_query_matches",
		"stratz_query_players",
		"stratz_server_info",
	}
	if len(definitions) != len(wantNames) {
		t.Fatalf("Definitions() count = %d, want %d", len(definitions), len(wantNames))
	}
	for i, definition := range definitions {
		if definition.Name != wantNames[i] {
			t.Fatalf("Definitions()[%d].Name = %q, want %q", i, definition.Name, wantNames[i])
		}
	}
	for _, definition := range definitions {
		inputSchema, err := Schema(definition.Name, InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		outputSchema, err := Schema(definition.Name, OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(inputSchema), `"$ref"`) || strings.Contains(string(outputSchema), `"$ref"`) {
			t.Fatalf("%s contains unresolved references", definition.Name)
		}

		input, err := Example(definition.Name, InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateInput(definition.Name, input); err != nil {
			t.Fatal(err)
		}
		output, err := Example(definition.Name, OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateOutput(definition.Name, output); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublishedDescriptionsMatchCanonicalRegistry(t *testing.T) {
	var registry struct {
		Tools map[string]struct {
			Description string `json:"description"`
		} `json:"tools"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "tool-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	for _, definition := range Definitions() {
		tool, ok := registry.Tools[definition.Name]
		if !ok {
			t.Fatalf("canonical registry is missing %s", definition.Name)
		}
		if definition.Description != tool.Description {
			t.Fatalf("%s description = %q, want canonical %q", definition.Name, definition.Description, tool.Description)
		}
	}
}

func TestGeneratedQuerySchemasEnforceModesBoundsAndOptInStatistics(t *testing.T) {
	validInputs := []struct {
		name  string
		value map[string]any
	}{
		{"stratz_query_players", map[string]any{"mode": "exact", "player_ids": []any{"1"}}},
		{"stratz_query_matches", map[string]any{"mode": "exact", "match_ids": []any{"1"}}},
		{"stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1"}},
		{"stratz_query_matches", map[string]any{"mode": "league_history", "league_id": "1"}},
		{"stratz_query_matches", map[string]any{"mode": "live"}},
		{"stratz_query_heroes", map[string]any{"mode": "exact", "heroes": []any{1}}},
		{"stratz_query_heroes", map[string]any{"mode": "search", "query": "anti"}},
		{"stratz_query_leagues", map[string]any{"mode": "exact", "league_ids": []any{"1"}}},
		{"stratz_query_leagues", map[string]any{"mode": "search"}},
		{"stratz_query_constants", map[string]any{"mode": "types", "types": []any{"heroes"}}},
		{"stratz_query_constants", map[string]any{"mode": "typed_selectors", "selectors": []any{map[string]any{"type": "heroes", "ids": []any{"1"}}}}},
	}
	for _, test := range validInputs {
		if err := ValidateInput(test.name, test.value); err != nil {
			t.Errorf("ValidateInput(%s) rejected valid mode: %v", test.name, err)
		}
	}

	invalidMixed := []struct {
		name  string
		value map[string]any
	}{
		{"stratz_query_matches", map[string]any{"mode": "exact", "match_ids": []any{"1"}, "player_id": "1"}},
		{"stratz_query_heroes", map[string]any{"mode": "exact", "heroes": []any{1}, "query": "anti"}},
		{"stratz_query_constants", map[string]any{"mode": "types", "types": []any{"heroes"}, "selectors": []any{}}},
	}
	for _, test := range invalidMixed {
		if err := ValidateInput(test.name, test.value); err == nil {
			t.Errorf("ValidateInput(%s) accepted mixed-mode input", test.name)
		}
	}

	players25 := make([]any, 25)
	for i := range players25 {
		players25[i] = "1"
	}
	if err := ValidateInput("stratz_query_players", map[string]any{"mode": "exact", "player_ids": players25}); err != nil {
		t.Fatalf("ValidateInput() rejected 25-item exact selector: %v", err)
	}
	players26 := append(append([]any(nil), players25...), "1")
	if err := ValidateInput("stratz_query_players", map[string]any{"mode": "exact", "player_ids": players26}); err == nil {
		t.Fatal("ValidateInput() accepted 26-item exact selector")
	}

	for _, name := range []string{"stratz_query_players", "stratz_query_heroes"} {
		schema, err := Schema(name, InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(schema), `"default": false`) {
			t.Fatalf("%s input schema does not publish statistics default-off behavior", name)
		}
	}
	if err := ValidateInput("stratz_query_heroes", map[string]any{"mode": "exact", "heroes": []any{1}, "include_statistics": true}); err != nil {
		t.Fatalf("ValidateInput() rejected opt-in statistics: %v", err)
	}
}

func TestGeneratedMatchOutputUsesModeTags(t *testing.T) {
	output, err := Example("stratz_query_matches", OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	matchOutput := output.(map[string]any)
	data := matchOutput["data"].(map[string]any)
	data["mode"] = "exact"
	if err := ValidateOutput("stratz_query_matches", matchOutput); err != nil {
		t.Fatalf("exact tagged match output rejected: %v", err)
	}
	data["mode"] = "player_history"
	data["items"] = []any{}
	data["page"] = map[string]any{"next_cursor": nil, "has_more": false}
	if err := ValidateOutput("stratz_query_matches", matchOutput); err != nil {
		t.Fatalf("player_history tagged match output rejected: %v", err)
	}
	data["mode"] = "live"
	if err := ValidateOutput("stratz_query_matches", matchOutput); err != nil {
		t.Fatalf("live tagged match output rejected: %v", err)
	}
	data["mode"] = "search"
	if err := ValidateOutput("stratz_query_matches", matchOutput); err == nil {
		t.Fatal("ValidateOutput() accepted an unknown match mode tag")
	}
}

func TestMatchHistoryOutputUsesDistinctClosedItemShapes(t *testing.T) {
	defaultOutput := matchHistoryOutput(t, "player_history", false)
	defaultData := defaultOutput["data"].(map[string]any)
	defaultItem := defaultData["items"].([]any)[0].(map[string]any)
	if _, present := defaultItem["player"]; present {
		t.Fatal("player-history default item unexpectedly contains player")
	}
	if err := ValidateOutput("stratz_query_matches", defaultOutput); err != nil {
		t.Fatalf("player-history default output rejected without player: %v", err)
	}

	playersOutput := matchHistoryOutput(t, "player_history", true)
	if err := ValidateOutput("stratz_query_matches", playersOutput); err != nil {
		t.Fatalf("player-history players-detail output rejected with player: %v", err)
	}

	leagueOutput := matchHistoryOutput(t, "league_history", true)
	if err := ValidateOutput("stratz_query_matches", leagueOutput); err == nil {
		t.Fatal("league-history output accepted a player property")
	}
}

func matchHistoryOutput(t *testing.T, mode string, includePlayer bool) map[string]any {
	t.Helper()
	value, err := Example("stratz_query_matches", OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("match output type = %T, want object", value)
	}
	data, ok := output["data"].(map[string]any)
	if !ok {
		t.Fatalf("match output data type = %T, want object", output["data"])
	}
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("match output items = %#v, want one item", data["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("match output item type = %T, want object", items[0])
	}
	delete(item, "players")
	if includePlayer {
		item["player"] = map[string]any{
			"account_id": "1",
			"hero_id":    1,
			"team":       "radiant",
			"position":   0,
			"kills":      1,
			"deaths":     0,
			"assists":    2,
			"networth":   nil,
			"level":      nil,
			"won":        true,
		}
	}
	data["mode"] = mode
	data["items"] = []any{item}
	data["page"] = map[string]any{"next_cursor": nil, "has_more": false}
	return output
}

func TestInputValidatorRejectsInvalidValue(t *testing.T) {
	err := ValidateInput("stratz_query_players", map[string]any{"mode": "exact", "player_ids": []any{""}})
	if err == nil {
		t.Fatal("ValidateInput() accepted an empty player identifier")
	}
}

func TestProtocolFixtureTextMirrorsStructuredContent(t *testing.T) {
	for _, definition := range Definitions() {
		raw, err := ProtocolFixture(definition.Name)
		if err != nil {
			t.Fatal(err)
		}
		fixture := raw.(map[string]any)
		response := fixture["response"].(map[string]any)
		result := response["result"].(map[string]any)
		content := result["content"].([]any)
		text := content[0].(map[string]any)["text"].(string)
		compact, err := json.Marshal(result["structuredContent"])
		if err != nil {
			t.Fatal(err)
		}
		if text != string(compact) {
			t.Fatalf("%s text mirror differs from structured content", definition.Name)
		}
	}
}

func TestUnknownToolFailsClosed(t *testing.T) {
	if _, err := Schema("missing", InputSchema); err == nil {
		t.Fatal("Schema() accepted an unknown tool")
	}
	if err := ValidateInput("missing", map[string]any{}); err == nil {
		t.Fatal("ValidateInput() accepted an unknown tool")
	}
}
