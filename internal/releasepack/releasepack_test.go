package releasepack

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		"scripts/check-public-readiness.sh",
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

func TestMakefileIncludesPublicReadiness(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !regexp.MustCompile(`(?m)^\.PHONY: .*?\bpublic-readiness\b`).MatchString(text) {
		t.Fatal("Makefile .PHONY list must include public-readiness")
	}
	if !regexp.MustCompile(`(?m)^public-readiness:\s*$`).MatchString(text) {
		t.Fatal("Makefile must define a public-readiness target")
	}
	if !regexp.MustCompile(`(?m)^check: .*?\bpublic-readiness\b`).MatchString(text) {
		t.Fatal("make check must depend on public-readiness")
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

func TestPublicReadinessAuditPassesOnMinimalRepository(t *testing.T) {
	root := filepath.Join("..", "..")
	repo := createPublicReadinessFixtureRepo(t, root)
	command := exec.Command(filepath.Join(repo, "scripts", "check-public-readiness.sh"))
	command.Dir = repo
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("public readiness audit failed: %v\n%s", err, result)
	}
}

func TestPublicReadinessAuditRejectsForbiddenTrackedFiles(t *testing.T) {
	root := filepath.Join("..", "..")
	scriptMessage := "tracked private/local files are present"
	localPathMessage := "tracked non-doc files contain machine-local absolute home paths"
	restrictedMessage := "restricted STRATZ artifacts are tracked"
	machineLocalFixture := "#!/bin/sh\n# " + strings.Join([]string{"/Users", "alex", "private"}, "/") + "\n"
	cases := []struct {
		name       string
		path       string
		content    string
		wantSubstr string
	}{
		{name: "planning output", path: "docs/plans/private.md", content: "local plan\n", wantSubstr: scriptMessage},
		{name: "legacy implementation plan", path: "docs/implementation-plan.md", content: "private plan\n", wantSubstr: scriptMessage},
		{name: "env file", path: ".env", content: "STRATZ_API_TOKEN=secret\n", wantSubstr: scriptMessage},
		{name: "dist binary", path: "dist/stratz-mcp", content: "artifact\n", wantSubstr: scriptMessage},
		{name: "tool cache", path: ".bin/genqlient", content: "artifact\n", wantSubstr: scriptMessage},
		{name: "local state", path: ".stratz-local/state.json", content: "{}\n", wantSubstr: scriptMessage},
		{name: "cache database", path: "cache.db", content: "not sqlite\n", wantSubstr: scriptMessage},
		{name: "restricted sentinel", path: ".stratz-restricted", content: "restricted\n", wantSubstr: restrictedMessage},
		{name: "machine local path content", path: "scripts/local-path.sh", content: machineLocalFixture, wantSubstr: localPathMessage},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := createPublicReadinessFixtureRepo(t, root)
			writeFixtureFile(t, repo, tc.path, tc.content, 0o644)
			gitAddAll(t, repo)
			command := exec.Command(filepath.Join(repo, "scripts", "check-public-readiness.sh"))
			command.Dir = repo
			result, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("expected public readiness audit to fail for %s", tc.path)
			}
			if !bytes.Contains(result, []byte(tc.wantSubstr)) {
				t.Fatalf("audit output %q does not contain %q", result, tc.wantSubstr)
			}
		})
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

func createPublicReadinessFixtureRepo(t *testing.T, sourceRoot string) string {
	t.Helper()
	repo := t.TempDir()
	runCommand(t, repo, "git", "init")
	runCommand(t, repo, "git", "config", "user.name", "Fixture User")
	runCommand(t, repo, "git", "config", "user.email", "fixture@example.com")

	writeFixtureFile(t, repo, "README.md", "# STRATZ MCP\n\nUnofficial, local-only MCP server for bounded access to the STRATZ GraphQL API. Public release is currently blocked pending explicit STRATZ API-use, caching, redistribution, attribution, and branding clearance.\n", 0o644)
	writeFixtureFile(t, repo, "docs/release.md", "# Release procedure\n\nPublic publishing is disabled until `go run ./cmd/release-clearance-check` succeeds against `docs/release-clearance.json`.\n", 0o644)
	writeFixtureFile(t, repo, "scripts/check-public-readiness.sh", readFile(t, filepath.Join(sourceRoot, "scripts", "check-public-readiness.sh")), 0o755)
	writeFixtureFile(t, repo, "scripts/check-restricted-artifacts.sh", readFile(t, filepath.Join(sourceRoot, "scripts", "check-restricted-artifacts.sh")), 0o755)

	gitAddAll(t, repo)
	return repo
}

func gitAddAll(t *testing.T, repo string) {
	t.Helper()
	runCommand(t, repo, "git", "add", ".")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func runCommand(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, result)
	}
}

func writeFixtureFile(t *testing.T, repo string, path string, content string, mode os.FileMode) {
	t.Helper()
	fullPath := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
