package schema

import (
	"context"
	"fmt"

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
	if err := WriteBundle(directory, files); err != nil {
		return Manifest{}, fmt.Errorf("write schema artifacts: %w", err)
	}
	return manifest, nil
}
