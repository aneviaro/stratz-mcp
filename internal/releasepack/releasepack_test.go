package releasepack

import (
	"os"
	"os/exec"
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

func TestPackageReleaseProducesArchivesChecksumsAndImageInput(t *testing.T) {
	root := filepath.Join("..", "..")
	output := filepath.Join(t.TempDir(), "release")
	imageOutput := filepath.Join(t.TempDir(), "image")
	command := exec.Command("sh", filepath.Join("scripts", "package-release.sh"))
	command.Dir = root
	command.Env = append(os.Environ(),
		"VERSION=v1.2.3",
		"REVISION=fixture-revision",
		"SCHEMA_VERSION=fixture-schema",
		"OUTPUT_DIR="+output,
		"IMAGE_OUTPUT_DIR="+imageOutput,
		"TARGETS=linux/amd64 windows/amd64",
	)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("package release: %v\n%s", err, result)
	}
	for _, path := range []string{
		"stratz-mcp_v1.2.3_linux_amd64.tar.gz",
		"stratz-mcp_v1.2.3_windows_amd64.zip",
		"checksums.txt",
		"release-metadata.json",
	} {
		if _, err := os.Stat(filepath.Join(output, path)); err != nil {
			t.Fatalf("missing release artifact %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(imageOutput, "stratz-mcp-linux-amd64")); err != nil {
		t.Fatalf("missing Linux image input: %v", err)
	}
	checksums, err := os.ReadFile(filepath.Join(output, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checksums), "linux_amd64.tar.gz") ||
		!strings.Contains(string(checksums), "windows_amd64.zip") {
		t.Fatalf("checksums = %s", checksums)
	}
	metadata, err := os.ReadFile(filepath.Join(output, "release-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metadata), `"version":"v1.2.3"`) ||
		!strings.Contains(string(metadata), `"revision":"fixture-revision"`) {
		t.Fatalf("release metadata = %s", metadata)
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
