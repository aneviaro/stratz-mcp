package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/aneviaro/stratz-mcp/internal/graphql/generated"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

// Pull fetches, deterministically generates, and securely writes a restricted
// local schema bundle.
func Pull(ctx context.Context, executor stratz.Executor, directory string) (Manifest, error) {
	document, err := Fetch(ctx, executor)
	if err != nil {
		return Manifest{}, err
	}
	files, manifest, err := Generate(document)
	if err != nil {
		return Manifest{}, fmt.Errorf("generate schema artifacts: %w", err)
	}
	constants, err := fetchConstants(ctx, executor)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch constants resources: %w", err)
	}
	for path, data := range constants {
		files[path] = data
		manifest.Artifacts[path] = Artifact{
			Path:   path,
			SHA256: digest(data),
			Bytes:  len(data),
		}
	}
	manifestJSON, err := marshalCanonical(manifest)
	if err != nil {
		return Manifest{}, fmt.Errorf("update schema manifest: %w", err)
	}
	files[ManifestFile] = append(manifestJSON, '\n')
	if err := WriteBundle(directory, files); err != nil {
		return Manifest{}, fmt.Errorf("write schema artifacts: %w", err)
	}
	return manifest, nil
}

func fetchConstants(ctx context.Context, executor stratz.Executor) (map[string][]byte, error) {
	budget, _ := stratz.NewRequestBudget(1)
	response, err := executor.Execute(ctx, budget, stratz.Request{
		Query:         generated.StratzGetConstants_Operation,
		OperationName: "StratzGetConstants",
		Variables:     map[string]any{},
		Mode:          stratz.ModeCurated,
		AllowRetries:  true,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("STRATZ returned no constants response")
	}
	var envelope struct {
		Constants map[string][]map[string]any `json:"constants"`
	}
	if err := json.Unmarshal(response.Data, &envelope); err != nil {
		return nil, fmt.Errorf("decode constants response: %w", err)
	}
	names := map[string]string{
		"heroes":    "heroes.json",
		"items":     "items.json",
		"abilities": "abilities.json",
		"gameModes": "game-modes.json",
		"regions":   "regions.json",
		"ranks":     "ranks.json",
	}
	files := make(map[string][]byte, len(names))
	for field, name := range names {
		items := envelope.Constants[field]
		sort.SliceStable(items, func(i, j int) bool {
			return fmt.Sprint(items[i]["id"]) < fmt.Sprint(items[j]["id"])
		})
		data, err := json.Marshal(items)
		if err != nil {
			return nil, fmt.Errorf("encode %s constants: %w", field, err)
		}
		files["constants/"+name] = append(data, '\n')
	}
	return files, nil
}
