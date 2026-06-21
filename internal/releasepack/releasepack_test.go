package releasepack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
	var workflow struct {
		Jobs map[string]struct {
			Needs       any    `yaml:"needs"`
			Environment string `yaml:"environment"`
			Steps       []struct {
				Run  string `yaml:"run"`
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	clearance, ok := workflow.Jobs["clearance"]
	if !ok {
		t.Fatal("release workflow has no clearance job")
	}
	foundGate := false
	for _, step := range clearance.Steps {
		if strings.Contains(step.Run, "go run ./cmd/release-clearance-check") {
			foundGate = true
		}
	}
	if !foundGate {
		t.Fatal("clearance job does not execute the release-clearance check")
	}
	for _, name := range []string{"artifacts", "image"} {
		job, ok := workflow.Jobs[name]
		if !ok {
			t.Fatalf("release workflow has no %s job", name)
		}
		if job.Needs != "clearance" {
			t.Fatalf("%s job needs = %#v, want clearance", name, job.Needs)
		}
		if job.Environment != "public-release" {
			t.Fatalf("%s job environment = %q, want public-release", name, job.Environment)
		}
	}
}

func TestContainerIsNonRootAndScratchBased(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	var instructions []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		instructions = append(instructions, line)
	}
	fromScratch := 0
	for _, instruction := range instructions {
		if strings.HasPrefix(instruction, "FROM scratch") {
			fromScratch++
		}
	}
	if fromScratch < 2 {
		t.Fatal("Dockerfile must use immutable scratch stages")
	}
	if !containsInstruction(instructions, "USER 65532:65532") {
		t.Fatal("Dockerfile must use a numeric non-root user")
	}
	if !containsInstruction(instructions, "COPY --chown=65532:65532 dist/image/cache /cache") {
		t.Fatal("Dockerfile must provision an owned cache directory")
	}
}

func containsInstruction(instructions []string, want string) bool {
	for _, instruction := range instructions {
		if instruction == want {
			return true
		}
	}
	return false
}
