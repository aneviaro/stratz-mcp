package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/app"
	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

type cliExecutor struct{}

func (cliExecutor) Execute(
	context.Context,
	*stratz.RequestBudget,
	stratz.Request,
) (*stratz.Response, error) {
	return &stratz.Response{}, nil
}

type schemaCLIExecutor struct {
	data json.RawMessage
}

func (executor schemaCLIExecutor) Execute(
	context.Context,
	*stratz.RequestBudget,
	stratz.Request,
) (*stratz.Response, error) {
	return &stratz.Response{HTTPStatus: 200, Data: executor.data}, nil
}

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
			Executor:     cliExecutor{},
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
	if !strings.Contains(stdout.String(), "upstream: STRATZ is reachable") {
		t.Fatalf("doctor did not report connectivity: %q", stdout.String())
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

func TestDoctorReportsCachePermissionsBeforeRuntimeRepairsThem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission behavior")
	}
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "stratz-mcp")
	if err := os.Mkdir(cacheDirectory, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cacheDirectory, 0o777); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"doctor"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			Environ:      []string{"STRATZ_API_TOKEN=fixture-token"},
			UserCacheDir: func() (string, error) { return root, nil },
			Executor:     cliExecutor{},
		},
	)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cache directory is accessible by group or others") {
		t.Fatalf("doctor output = %q", stdout.String())
	}
}

func TestServeRunsUntilStdinClosesWithoutLeakingCredential(t *testing.T) {
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
			Stdin:        strings.NewReader(""),
			Executor:     cliExecutor{},
		},
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "fixture-token") {
		t.Fatalf("stderr leaked token: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no protocol output without input", stdout.String())
	}
}

func TestServeStopsWhenContextIsCanceledWithOpenStdin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	cacheRoot := t.TempDir()
	defer reader.Close()
	defer writer.Close()
	done := make(chan int, 1)
	go func() {
		done <- RunWithDependencies(
			[]string{"serve"},
			io.Discard,
			io.Discard,
			app.BuildInfo{},
			Dependencies{
				Context:      ctx,
				Environ:      []string{"STRATZ_API_TOKEN=fixture-token"},
				UserCacheDir: func() (string, error) { return cacheRoot, nil },
				Stdin:        reader,
				Executor:     cliExecutor{},
			},
		)
	}()
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
}

func TestSchemaPullGeneratesRestrictedLocalBundle(t *testing.T) {
	directory := t.TempDir()
	data, err := os.ReadFile("../schema/testdata/introspection-data.json")
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"schema", "pull"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			Environ:      []string{"STRATZ_API_TOKEN=fixture-token"},
			UserCacheDir: func() (string, error) { return directory, nil },
			Executor:     schemaCLIExecutor{data: data},
		},
	)
	if code != 0 {
		t.Fatalf("exit code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "restricted local data; do not publish") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	for _, relative := range []string{
		"schema/manifest.json",
		"schema/.stratz-restricted",
		"schema/schema/full.graphql",
		"schema/constants/heroes.json",
		"schema/constants/items.json",
		"schema/constants/abilities.json",
		"schema/constants/game-modes.json",
		"schema/constants/regions.json",
		"schema/constants/ranks.json",
	} {
		if _, err := os.Stat(filepath.Join(directory, "stratz-mcp", relative)); err != nil {
			t.Errorf("missing %s: %v", relative, err)
		}
	}
}

func TestSchemaPullRejectsInvalidSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"schema", "unknown"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{},
	)
	if code != 2 || !strings.Contains(stderr.String(), "usage: stratz-mcp schema pull") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestCacheStatsAndClearCurrentToken(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Defaults(directory)
	store, err := cache.Open(cache.Options{
		Config:   cfg.Cache,
		Features: cfg.Features,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, class, err := cacheEntryForTest(cfg, "fixture-token", "heroes")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), cache.Entry{
		Key:            key,
		Classification: class,
		Payload:        []byte(`{"hero_id":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithDependencies(
		[]string{"cache", "stats"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			Environ:      []string{},
			UserCacheDir: func() (string, error) { return directory, nil },
		},
	)
	if code != 0 {
		t.Fatalf("stats code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "status: healthy") || !strings.Contains(stdout.String(), "entries: 1") {
		t.Fatalf("stats stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = RunWithDependencies(
		[]string{"cache", "clear", "--current-token"},
		&stdout,
		&stderr,
		app.BuildInfo{},
		Dependencies{
			Environ:      []string{"STRATZ_API_TOKEN=fixture-token"},
			UserCacheDir: func() (string, error) { return directory, nil },
		},
	)
	if code != 0 {
		t.Fatalf("clear code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if stdout.String() != "deleted: 1\n" {
		t.Fatalf("clear stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), cache.NamespaceForToken("fixture-token")) {
		t.Fatalf("clear output leaked namespace: %q", stdout.String())
	}
}

func cacheEntryForTest(
	cfg config.Config,
	token, domain string,
) (cache.Key, cache.Classification, error) {
	classification := cache.ResolveClassification(
		cfg.Cache,
		cfg.Features,
		domain,
		cache.ClassPublicReference,
		false,
	)
	key, err := cache.CanonicalKey(cache.KeyInput{
		Namespace:     cache.NamespaceForToken(token),
		Domain:        domain,
		Class:         cache.ClassPublicReference,
		Operation:     "test",
		Arguments:     map[string]any{"id": 1},
		SchemaVersion: "sha256:test",
	})
	return key, classification, err
}
