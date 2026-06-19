package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeneratedContractsAreCompleteAndValidateExamples(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 15 {
		t.Fatalf("Definitions() count = %d, want 15", len(definitions))
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

func TestInputValidatorRejectsInvalidValue(t *testing.T) {
	err := ValidateInput("stratz_get_player", map[string]any{"player_id": ""})
	if err == nil {
		t.Fatal("ValidateInput() accepted an empty player_id")
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
