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
		{"stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1", "role": "CORE", "position": "POSITION_1", "lane": "ROAMING", "patch_id": "2147483647"}},
		{"stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1", "role": "LIGHT_SUPPORT", "position": "POSITION_2", "lane": "SAFE_LANE", "patch_id": "739"}},
		{"stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1", "role": "HARD_SUPPORT", "position": "POSITION_3", "lane": "MID_LANE", "patch_id": "1"}},
		{"stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1", "position": "POSITION_4", "lane": "OFF_LANE"}},
		{"stratz_query_matches", map[string]any{"mode": "player_history", "player_id": "1", "position": "POSITION_5", "lane": "JUNGLE"}},
		{"stratz_query_matches", map[string]any{"mode": "league_history", "league_id": "1", "patch_id": "1"}},
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
	invalidHistoryFilters := []map[string]any{
		{"mode": "player_history", "player_id": "1", "role": "POSITION_1"},
		{"mode": "player_history", "player_id": "1", "role": 1},
		{"mode": "player_history", "player_id": "1", "role": "UNKNOWN"},
		{"mode": "player_history", "player_id": "1", "position": "POSITION_6"},
		{"mode": "player_history", "player_id": "1", "lane": "UNKNOWN"},
		{"mode": "player_history", "player_id": "1", "patch_id": "7.34"},
		{"mode": "player_history", "player_id": "1", "patch_id": "abc"},
		{"mode": "player_history", "player_id": "1", "patch_id": "0"},
		{"mode": "player_history", "player_id": "1", "patch_id": "-1"},
		{"mode": "player_history", "player_id": "1", "patch_id": "2147483648"},
	}
	for _, value := range invalidHistoryFilters {
		if err := ValidateInput("stratz_query_matches", value); err == nil {
			t.Errorf("ValidateInput(stratz_query_matches) accepted invalid history filter: %#v", value)
		}
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

func TestMatchPlayerIMPValuesValidatePublicContract(t *testing.T) {
	for _, imp := range []any{4.25, -3.5, nil} {
		t.Run(impLabel(imp), func(t *testing.T) {
			historyOutput := matchHistoryOutput(t, "player_history", true)
			historyData := historyOutput["data"].(map[string]any)
			historyItem := historyData["items"].([]any)[0].(map[string]any)
			historyPlayer := historyItem["player"].(map[string]any)
			historyPlayer["imp"] = imp
			if err := ValidateOutput("stratz_query_matches", historyOutput); err != nil {
				t.Fatalf("player-history output with imp=%#v rejected: %v", imp, err)
			}

			exactValue, err := Example("stratz_query_matches", OutputSchema)
			if err != nil {
				t.Fatal(err)
			}
			exactOutput := exactValue.(map[string]any)
			exactData := exactOutput["data"].(map[string]any)
			exactItem := exactData["items"].([]any)[0].(map[string]any)
			exactItem["players"] = []any{map[string]any{
				"account_id":  "1",
				"hero_id":     1,
				"team":        "radiant",
				"player_slot": 0,
				"position":    "POSITION_1",
				"role":        "CORE",
				"role_basic":  "CORE",
				"lane":        "SAFE_LANE",
				"party_id":    1,
				"kills":       1,
				"deaths":      0,
				"assists":     2,
				"networth":    nil,
				"level":       nil,
				"won":         true,
				"imp":         imp,
			}}
			if err := ValidateOutput("stratz_query_matches", exactOutput); err != nil {
				t.Fatalf("exact output with imp=%#v rejected: %v", imp, err)
			}
		})
	}
}

func TestMatchPlayerMetadataSchemaAndNullability(t *testing.T) {
	schema, err := Schema("stratz_query_matches", OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(schema, &document); err != nil {
		t.Fatal(err)
	}
	var matchPlayerSchemas []map[string]any
	var visit func(any)
	visit = func(value any) {
		node, ok := value.(map[string]any)
		if !ok {
			if values, ok := value.([]any); ok {
				for _, child := range values {
					visit(child)
				}
			}
			return
		}
		if properties, ok := node["properties"].(map[string]any); ok {
			if _, ok := properties["player_slot"]; ok {
				matchPlayerSchemas = append(matchPlayerSchemas, node)
			}
		}
		for _, child := range node {
			visit(child)
		}
	}
	visit(document)
	if len(matchPlayerSchemas) == 0 {
		t.Fatal("generated match output schema does not contain matchPlayer")
	}
	for _, playerSchema := range matchPlayerSchemas {
		required, ok := playerSchema["required"].([]any)
		if !ok {
			t.Fatal("matchPlayer schema has no required list")
		}
		requiredSet := make(map[string]bool, len(required))
		for _, field := range required {
			if name, ok := field.(string); ok {
				requiredSet[name] = true
			}
		}
		for _, field := range []string{"player_slot", "position", "role", "role_basic", "lane", "party_id"} {
			if !requiredSet[field] {
				t.Errorf("matchPlayer schema does not require %q", field)
			}
		}
	}

	output := matchHistoryOutput(t, "player_history", true)
	player := output["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["player"].(map[string]any)
	for field, value := range map[string]any{
		"position":   "UNKNOWN",
		"role":       "UNKNOWN",
		"role_basic": "UNKNOWN",
		"lane":       "UNKNOWN",
		"party_id":   0,
	} {
		player[field] = value
	}
	if err := ValidateOutput("stratz_query_matches", output); err != nil {
		t.Fatalf("UNKNOWN match-player metadata rejected: %v", err)
	}
	player["position"] = "POSITION_3"
	player["role"] = "CORE"
	player["role_basic"] = "LIGHT_SUPPORT"
	player["lane"] = "OFF_LANE"
	player["party_id"] = nil
	if err := ValidateOutput("stratz_query_matches", output); err != nil {
		t.Fatalf("independent role/role_basic/lane and null party metadata rejected: %v", err)
	}
	for _, field := range []string{"position", "role", "role_basic", "lane", "party_id"} {
		player[field] = nil
	}
	if err := ValidateOutput("stratz_query_matches", output); err != nil {
		t.Fatalf("null match-player metadata rejected: %v", err)
	}
	player["position"] = 0
	if err := ValidateOutput("stratz_query_matches", output); err == nil {
		t.Fatal("match-player schema accepted the old integer position")
	}
	player["position"] = "POSITION_1"
	player["party_id"] = -1
	if err := ValidateOutput("stratz_query_matches", output); err == nil {
		t.Fatal("match-player schema accepted a negative party_id")
	}
}

func impLabel(imp any) string {
	if imp == nil {
		return "null"
	}
	if imp.(float64) < 0 {
		return "negative"
	}
	return "positive"
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
			"account_id":  "1",
			"hero_id":     1,
			"team":        "radiant",
			"player_slot": 0,
			"position":    "POSITION_1",
			"role":        "CORE",
			"role_basic":  "CORE",
			"lane":        "SAFE_LANE",
			"party_id":    1,
			"kills":       1,
			"deaths":      0,
			"assists":     2,
			"networth":    nil,
			"level":       nil,
			"won":         true,
			"imp":         nil,
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
