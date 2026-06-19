package contractgen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildGeneratesCompleteDeterministicContract(t *testing.T) {
	registryPath := filepath.Join("..", "..", "docs", "tool-contracts.json")
	first, err := Build(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 78 {
		t.Fatalf("artifact count = %d, want 78", len(first))
	}
	for i := range first {
		if first[i].Path != second[i].Path || !bytes.Equal(first[i].Data, second[i].Data) {
			t.Fatalf("artifact %d is not deterministic: %s vs %s", i, first[i].Path, second[i].Path)
		}
	}

	schemaCount := 0
	for _, artifact := range first {
		if strings.Contains(artifact.Path, "/schemas/") {
			schemaCount++
			if bytes.Contains(artifact.Data, []byte(`"$ref"`)) {
				t.Fatalf("%s contains an unresolved reference", artifact.Path)
			}
			if !bytes.Contains(artifact.Data, []byte(draft202012)) {
				t.Fatalf("%s does not declare Draft 2020-12", artifact.Path)
			}
			var schema map[string]any
			if err := json.Unmarshal(artifact.Data, &schema); err != nil {
				t.Fatalf("decode %s: %v", artifact.Path, err)
			}
			if schema["type"] != "object" {
				t.Fatalf("%s root type = %v, want object", artifact.Path, schema["type"])
			}
		}
	}
	if schemaCount != 30 {
		t.Fatalf("schema count = %d, want 30", schemaCount)
	}
}

func TestValidateRegistryRejectsUnsupportedKeyword(t *testing.T) {
	reg := readTestRegistry(t)
	tool := reg.Tools["stratz_server_info"]
	tool.InputSchema.(map[string]any)["unevaluatedProperties"] = false
	reg.Tools["stratz_server_info"] = tool

	err := validateRegistry(reg)
	if err == nil || !strings.Contains(err.Error(), "unsupported JSON Schema keyword") {
		t.Fatalf("validateRegistry() error = %v", err)
	}
}

func TestValidateRegistryRejectsContractVersionDrift(t *testing.T) {
	reg := readTestRegistry(t)
	reg.ContractVersion = "2.0.0"

	err := validateRegistry(reg)
	if err == nil || !strings.Contains(err.Error(), "generator expects") {
		t.Fatalf("validateRegistry() error = %v", err)
	}
}

func TestValidateRegistryRejectsUnsafeRawGraphQLPolicy(t *testing.T) {
	reg := readTestRegistry(t)
	reg.RawGraphQLPolicy.RootFieldDefault = "allow"

	err := validateRegistry(reg)
	if err == nil || !strings.Contains(err.Error(), "default-deny") {
		t.Fatalf("validateRegistry() error = %v", err)
	}
}

func TestGenerateMatchesCheckedInArtifacts(t *testing.T) {
	root := filepath.Join("..", "..")
	expected, err := Build(filepath.Join(root, contractRegistryPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range expected {
		data, err := os.ReadFile(filepath.Join(root, artifact.Path))
		if err != nil {
			t.Fatalf("read %s: %v; run go generate ./...", artifact.Path, err)
		}
		if !bytes.Equal(data, artifact.Data) {
			t.Fatalf("%s is stale; run go generate ./...", artifact.Path)
		}
	}
}

func TestExpectedToolsStaySorted(t *testing.T) {
	if !slices.IsSorted(expectedTools) {
		t.Fatal("expectedTools must remain sorted")
	}
}

func readTestRegistry(t *testing.T) registry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", contractRegistryPath))
	if err != nil {
		t.Fatal(err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatal(err)
	}
	return reg
}
