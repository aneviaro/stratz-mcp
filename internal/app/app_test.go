package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

func TestBuildInfoNormalized(t *testing.T) {
	info := (BuildInfo{}).Normalized()
	if info.Version != DevelopmentVersion {
		t.Fatalf("version = %q, want %q", info.Version, DevelopmentVersion)
	}
	if info.Revision != UnknownRevision {
		t.Fatalf("revision = %q, want %q", info.Revision, UnknownRevision)
	}
	if info.SchemaVersion != UnknownSchema {
		t.Fatalf("schema version = %q, want %q", info.SchemaVersion, UnknownSchema)
	}
}

func TestBuildInfoString(t *testing.T) {
	info := BuildInfo{
		Version:       "v1.2.3",
		Revision:      "abc123",
		SchemaVersion: "sha256:fixture",
	}
	const want = "version=v1.2.3 revision=abc123 schema_version=sha256:fixture"
	if got := info.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

type runtimeExecutor struct{}

func (runtimeExecutor) Execute(
	context.Context,
	*stratz.RequestBudget,
	stratz.Request,
) (*stratz.Response, error) {
	return &stratz.Response{}, nil
}

func TestNewRuntimeComposesServerAndExecutor(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	executor := runtimeExecutor{}
	runtime, err := NewRuntime(RuntimeOptions{
		Build: BuildInfo{
			Version:       "v1.2.3",
			SchemaVersion: "sha256:fixture",
		},
		Config:     cfg,
		Credential: auth.Credential{Token: "unused"},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Executor:   executor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Server() == nil {
		t.Fatal("runtime server is nil")
	}
	if runtime.Executor() == nil {
		t.Fatal("runtime executor is nil")
	}
	if runtime.Cache() == nil {
		t.Fatal("runtime cache is nil")
	}
	if runtime.Cache().Status() != cache.StatusHealthy {
		t.Fatalf("cache status = %q, want healthy", runtime.Cache().Status())
	}
}

func TestNewRuntimeFallsBackWhenCacheInitializationFails(t *testing.T) {
	directory := t.TempDir()
	cacheRoot := filepath.Join(directory, "cache-root")
	if err := os.WriteFile(cacheRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults(directory)
	cfg.Cache.Directory = cacheRoot

	runtime, err := NewRuntime(RuntimeOptions{
		Build: BuildInfo{
			Version:       "v1.2.3",
			SchemaVersion: "sha256:fixture",
		},
		Config:     cfg,
		Credential: auth.Credential{Token: "unused"},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Executor:   runtimeExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Cache().Status() != cache.StatusDegraded {
		t.Fatalf("cache status = %q, want degraded", runtime.Cache().Status())
	}
}
