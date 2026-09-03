// Package app composes the STRATZ MCP application's runtime dependencies.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	mcpserver "github.com/aneviaro/stratz-mcp/internal/mcp"
	"github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const (
	// DevelopmentVersion is used when no release version is injected.
	DevelopmentVersion = "dev"
	// UnknownRevision is used when no source revision is injected.
	UnknownRevision = "unknown"
	// UnknownSchema is used when no schema manifest is available.
	UnknownSchema = "unavailable"
)

// BuildInfo contains metadata supplied by the build pipeline.
type BuildInfo struct {
	Version       string
	Revision      string
	SchemaVersion string
}

// RuntimeOptions supplies the production composition root while allowing
// bounded test doubles at package boundaries.
type RuntimeOptions struct {
	Build      BuildInfo
	Config     config.Config
	Credential auth.Credential
	Logger     *slog.Logger
	Executor   stratz.Executor
	Now        func() time.Time
	Handlers   map[string]mcpserver.ToolHandler
}

// Runtime owns the application services needed by CLI commands.
type Runtime struct {
	server   *mcpserver.Server
	cache    *cache.Store
	executor stratz.Executor
	build    BuildInfo
}

// NewRuntime composes the STRATZ client and static MCP server.
func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	build := options.Build.Normalized()
	if manifest, err := schema.LoadManifest(schemaDirectory(options.Config.Cache.Directory)); err == nil {
		build.SchemaVersion = manifest.SchemaHash
	}
	executor := options.Executor
	if executor == nil {
		client, err := stratz.New(options.Credential, build.Version, options.Config.Limits)
		if err != nil {
			return nil, fmt.Errorf("construct STRATZ client: %w", err)
		}
		executor = client
	}
	cacheStore, err := cache.Open(cache.Options{
		Config:          options.Config.Cache,
		Features:        options.Config.Features,
		Logger:          options.Logger,
		Now:             options.Now,
		MaxPayloadBytes: options.Config.Limits.MaxResponseBytes,
	})
	if err != nil {
		if options.Logger != nil {
			options.Logger.Warn("cache initialization failed; continuing without cache", "error", err)
		}
		cacheStore = cache.Degraded(options.Logger, err.Error())
	}
	server, err := mcpserver.New(mcpserver.Options{
		Version:         build.Version,
		SchemaVersion:   build.SchemaVersion,
		SchemaDirectory: schemaDirectory(options.Config.Cache.Directory),
		CacheStatus:     cacheStore.Status(),
		Cache:           cacheStore,
		CacheNamespace:  cache.NamespaceForToken(options.Credential.Token),
		Config:          options.Config,
		Executor:        executor,
		CursorToken:     options.Credential.Token,
		Logger:          options.Logger,
		Now:             options.Now,
		Handlers:        options.Handlers,
	})
	if err != nil {
		if cacheStore != nil {
			if closeErr := cacheStore.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close cache store: %w", closeErr))
			}
		}
		return nil, fmt.Errorf("construct MCP server: %w", err)
	}
	return &Runtime{
		server:   server,
		cache:    cacheStore,
		executor: executor,
		build:    build,
	}, nil
}

// Build returns the effective runtime metadata, including a pulled schema hash.
func (r *Runtime) Build() BuildInfo {
	return r.build
}

// Close flushes asynchronous cache writes and releases runtime resources.
func (r *Runtime) Close() error {
	if r == nil || r.cache == nil {
		return nil
	}
	return r.cache.Close()
}

func schemaDirectory(cacheDirectory string) string {
	if strings.TrimSpace(cacheDirectory) == "" {
		return ""
	}
	return filepath.Join(cacheDirectory, "schema")
}

// Executor returns the bounded upstream dependency used by runtime commands.
func (r *Runtime) Executor() stratz.Executor {
	return r.executor
}

// Cache returns the process cache backend state.
func (r *Runtime) Cache() *cache.Store {
	return r.cache
}

// Server returns the configured MCP adapter.
func (r *Runtime) Server() *mcpserver.Server {
	return r.server
}

// Serve runs the stdio server. The output wrapper deliberately has a no-op
// close so the process owns stdout for its full lifetime.
func (r *Runtime) Serve(
	ctx context.Context,
	stdin io.Reader,
	stdout io.Writer,
) error {
	reader, ok := stdin.(io.ReadCloser)
	if !ok {
		reader = io.NopCloser(stdin)
	}
	return r.server.Run(ctx, reader, writerCloser{Writer: stdout})
}

type writerCloser struct {
	io.Writer
}

func (writerCloser) Close() error { return nil }

// Normalized replaces empty linker-provided values with safe development
// defaults.
func (info BuildInfo) Normalized() BuildInfo {
	info.Version = valueOrDefault(info.Version, DevelopmentVersion)
	info.Revision = valueOrDefault(info.Revision, UnknownRevision)
	info.SchemaVersion = valueOrDefault(info.SchemaVersion, UnknownSchema)
	return info
}

func (info BuildInfo) String() string {
	info = info.Normalized()
	return fmt.Sprintf(
		"version=%s revision=%s schema_version=%s",
		info.Version,
		info.Revision,
		info.SchemaVersion,
	)
}

func valueOrDefault(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
