package contractgen

import (
	"bytes"
	"encoding/json"
	"io/fs"
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
	catalog := readTestRegistry(t)
	wantArtifactCount := len(catalog.Tools)*5 + 3
	if len(first) != wantArtifactCount {
		t.Fatalf("artifact count = %d, want %d", len(first), wantArtifactCount)
	}
	artifactPaths := make(map[string]bool, len(first))
	for _, artifact := range first {
		artifactPaths[artifact.Path] = true
	}
	for _, name := range expectedTools {
		for _, path := range []string{
			"internal/contracts/generated/schemas/" + name + ".input.json",
			"internal/contracts/generated/schemas/" + name + ".output.json",
			"internal/contracts/generated/examples/" + name + ".input.json",
			"internal/contracts/generated/examples/" + name + ".output.json",
			"internal/contracts/generated/protocol/" + name + ".json",
		} {
			if !artifactPaths[path] {
				t.Fatalf("required artifact %s is missing", path)
			}
		}
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
	if schemaCount != len(expectedTools)*2 {
		t.Fatalf("schema count = %d, want %d", schemaCount, len(expectedTools)*2)
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
	reg.ContractVersion = "1.0.0"

	err := validateRegistry(reg)
	if err == nil || !strings.Contains(err.Error(), "generator expects") {
		t.Fatalf("validateRegistry() error = %v", err)
	}
}

func TestValidateRegistryRejectsDescriptionOverLimit(t *testing.T) {
	reg := readTestRegistry(t)
	tool := reg.Tools["stratz_server_info"]
	tool.Description = strings.Repeat("x", maxDescriptionBytes+1)
	reg.Tools["stratz_server_info"] = tool

	err := validateRegistry(reg)
	if err == nil || !strings.Contains(err.Error(), "description exceeds") {
		t.Fatalf("validateRegistry() error = %v", err)
	}
}

func TestValidateRegistryRejectsInvalidProtocolMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*registry)
	}{
		{
			name: "empty",
			mutate: func(reg *registry) {
				reg.SupportedMCPProtocolVersions = nil
			},
		},
		{
			name: "duplicate",
			mutate: func(reg *registry) {
				reg.SupportedMCPProtocolVersions[1] = reg.SupportedMCPProtocolVersions[0]
			},
		},
		{
			name: "misordered",
			mutate: func(reg *registry) {
				reg.SupportedMCPProtocolVersions[0], reg.SupportedMCPProtocolVersions[1] = reg.SupportedMCPProtocolVersions[1], reg.SupportedMCPProtocolVersions[0]
			},
		},
		{
			name: "unsupported",
			mutate: func(reg *registry) {
				reg.SupportedMCPProtocolVersions[1] = "1999-01-01"
			},
		},
		{
			name: "preferred mismatch",
			mutate: func(reg *registry) {
				reg.MCPProtocolVersion = "2025-11-25"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reg := readTestRegistry(t)
			test.mutate(&reg)
			if err := validateRegistry(reg); err == nil {
				t.Fatal("validateRegistry() unexpectedly succeeded")
			}
		})
	}
}

func TestValidateRegistryRejectsServerInfoSchemaDrift(t *testing.T) {
	reg := readTestRegistry(t)
	tool := reg.Tools["stratz_server_info"]
	output := tool.OutputSchema.(map[string]any)
	alternatives := output["oneOf"].([]any)
	allOf := alternatives[0].(map[string]any)["allOf"].([]any)
	properties := allOf[1].(map[string]any)["properties"].(map[string]any)
	data := properties["data"].(map[string]any)
	dataProperties := data["properties"].(map[string]any)
	dataProperties["mcp_protocol_version"].(map[string]any)["const"] = "2025-11-25"
	if err := validateRegistry(reg); err == nil || !strings.Contains(err.Error(), "mcp_protocol_version schema") {
		t.Fatalf("validateRegistry() error = %v", err)
	}
}

func TestValidateRegistryRejectsSupportedProtocolSchemaDrift(t *testing.T) {
	reg := readTestRegistry(t)
	tool := reg.Tools["stratz_server_info"]
	output := tool.OutputSchema.(map[string]any)
	alternatives := output["oneOf"].([]any)
	allOf := alternatives[0].(map[string]any)["allOf"].([]any)
	properties := allOf[1].(map[string]any)["properties"].(map[string]any)
	data := properties["data"].(map[string]any)
	dataProperties := data["properties"].(map[string]any)
	items := dataProperties["supported_mcp_protocol_versions"].(map[string]any)["items"].(map[string]any)
	items["enum"] = []any{"2026-07-28", "1999-01-01", "2025-06-18", "2025-03-26", "2024-11-05"}
	if err := validateRegistry(reg); err == nil || !strings.Contains(err.Error(), "supported protocol schema") {
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
	expectedPaths := make(map[string]struct{}, len(expected))
	for _, artifact := range expected {
		expectedPaths[filepath.ToSlash(artifact.Path)] = struct{}{}
		data, err := os.ReadFile(filepath.Join(root, artifact.Path))
		if err != nil {
			t.Fatalf("read %s: %v; run go generate ./...", artifact.Path, err)
		}
		if !bytes.Equal(data, artifact.Data) {
			t.Fatalf("%s is stale; run go generate ./...", artifact.Path)
		}
	}
	generatedRoot := filepath.Join(root, "internal", "contracts", "generated")
	err = filepath.WalkDir(generatedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, ok := expectedPaths[filepath.ToSlash(relative)]; !ok {
			t.Errorf("obsolete generated artifact %s remains", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
