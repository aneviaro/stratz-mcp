package releasepack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSurface(t *testing.T) {
	root := filepath.Join("..", "..")
	required := []string{
		"Dockerfile",
		"LICENSE",
		"README.md",
		"SECURITY.md",
		"THIRD_PARTY_NOTICES",
		".github/workflows/release.yml",
		".github/workflows/security.yml",
		"scripts/package-release.sh",
		"scripts/interop-smoke.sh",
		"docs/release.md",
		"docs/interoperability.md",
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("required release file %s: %v", name, err)
		}
	}
}

func TestPublishingRequiresClearance(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	gate := "go run ./cmd/release-clearance-check"
	if !strings.Contains(text, gate) {
		t.Fatalf("release workflow does not invoke %q", gate)
	}
	if strings.Index(text, gate) > strings.Index(text, "softprops/action-gh-release") {
		t.Fatal("clearance check must precede publication")
	}
	if !strings.Contains(text, "environment: public-release") {
		t.Fatal("publishing jobs must require protected environment approval")
	}
}

func TestContainerIsNonRootAndScratchBased(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Count(text, "FROM scratch") < 2 {
		t.Fatal("Dockerfile must use immutable scratch stages")
	}
	if !strings.Contains(text, "USER 65532:65532") {
		t.Fatal("Dockerfile must use a numeric non-root user")
	}
}
