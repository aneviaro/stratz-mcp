package cli

import (
	"bytes"
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
