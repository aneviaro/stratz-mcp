package schema

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/securefile"
)

// LoadManifest securely loads and minimally validates a generated schema manifest.
func LoadManifest(directory string) (Manifest, error) {
	if strings.TrimSpace(directory) == "" {
		return Manifest{}, fmt.Errorf("schema directory is required")
	}
	file, err := securefile.OpenReadOnly(filepath.Join(directory, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 8<<20))
	if err != nil {
		return Manifest{}, fmt.Errorf("read schema manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode schema manifest: %w", err)
	}
	if manifest.FormatVersion != FormatVersion || strings.TrimSpace(manifest.SchemaHash) == "" {
		return Manifest{}, fmt.Errorf("schema manifest is incompatible")
	}
	return manifest, nil
}
