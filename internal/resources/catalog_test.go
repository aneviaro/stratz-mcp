package resources

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDefinitionsContainRequiredURIs(t *testing.T) {
	want := []string{
		"stratz://schema/full",
		"stratz://schema/player",
		"stratz://schema/match",
		"stratz://schema/hero",
		"stratz://schema/league",
		"stratz://schema/live",
		"stratz://schema/constants",
		"stratz://constants/heroes",
		"stratz://constants/items",
		"stratz://constants/abilities",
		"stratz://constants/game-modes",
		"stratz://constants/regions",
		"stratz://constants/ranks",
	}
	definitions := Definitions()
	if len(definitions) != len(want) {
		t.Fatalf("definition count = %d, want %d", len(definitions), len(want))
	}
	for index, uri := range want {
		if definitions[index].URI != uri {
			t.Errorf("definition %d URI = %q, want %q", index, definitions[index].URI, uri)
		}
	}
}

func TestCatalogReadsBoundedLocalArtifact(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "schema", "full.graphql")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("type Query { ok: Boolean }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := New(directory)
	handler := catalog.handler(definitions[0])
	result, err := handler(context.Background(), &sdk.ReadResourceRequest{
		Params: &sdk.ReadResourceParams{URI: definitions[0].URI},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 ||
		result.Contents[0].Text != "type Query { ok: Boolean }\n" ||
		result.Contents[0].MIMEType != "application/graphql" {
		t.Fatalf("resource result = %#v", result)
	}
}

func TestCatalogRejectsMissingAndOversizedArtifacts(t *testing.T) {
	catalog := New(t.TempDir())
	handler := catalog.handler(definitions[0])
	if _, err := handler(context.Background(), &sdk.ReadResourceRequest{
		Params: &sdk.ReadResourceParams{URI: definitions[0].URI},
	}); err == nil {
		t.Fatal("missing resource was readable")
	}

	directory := t.TempDir()
	path := filepath.Join(directory, "schema", "full.graphql")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, maxResourceBytes+1)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(directory).handler(definitions[0])(
		context.Background(),
		&sdk.ReadResourceRequest{
			Params: &sdk.ReadResourceParams{URI: definitions[0].URI},
		},
	); err == nil {
		t.Fatal("oversized resource was readable")
	}
}
