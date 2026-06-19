package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/app"
)

func TestRunDisplaysHelpWithoutSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run(nil, &stdout, &stderr, app.BuildInfo{}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage: stratz-mcp <command>") {
		t.Fatalf("stdout did not contain usage: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunDisplaysInjectedVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	info := app.BuildInfo{
		Version:       "v1.2.3",
		Revision:      "abc123",
		SchemaVersion: "sha256:fixture",
	}

	if code := Run([]string{"version"}, &stdout, &stderr, info); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got, want := stdout.String(), "stratz-mcp version=v1.2.3 revision=abc123 schema_version=sha256:fixture\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run([]string{"unknown"}, &stdout, &stderr, app.BuildInfo{}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("stderr did not contain unknown-command error: %q", stderr.String())
	}
}

func TestVersionDoesNotRequireConfigurationOrCredentials(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"version"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			Environ: []string{
				"STRATZ_CONFIG_FILE=/does/not/exist",
				"STRATZ_API_TOKEN=fixture-token",
				"STRATZ_API_TOKEN_FILE=/conflicting/path",
			},
			UserCacheDir: func() (string, error) { return t.TempDir(), nil },
		},
	)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stratz-mcp version=") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDoctorChecksLocalConfigurationAndPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission behavior")
	}
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	if err := os.WriteFile(tokenPath, []byte("fixture-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"doctor", "--token-file", tokenPath},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			UserCacheDir: func() (string, error) { return directory, nil },
		},
	)
	if code != 0 {
		t.Fatalf("exit code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "credentials: valid (file source)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), tokenPath) || strings.Contains(stdout.String(), "fixture-token") {
		t.Fatalf("doctor output leaked credential details: %q", stdout.String())
	}
}

func TestDoctorFailsWhenCredentialIsAbsent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"doctor"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			UserCacheDir: func() (string, error) { return t.TempDir(), nil },
		},
	)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "token is absent") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestServeRedactsCredentialFromLogs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"serve", "--log-format", "json"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			Environ:      []string{"STRATZ_API_TOKEN=fixture-token"},
			UserCacheDir: func() (string, error) { return t.TempDir(), nil },
		},
	)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if strings.Contains(stderr.String(), "fixture-token") {
		t.Fatalf("stderr leaked token: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "not implemented") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
