// Package app composes the STRATZ MCP application's runtime dependencies.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/config"
	mcpserver "github.com/aneviaro/stratz-mcp/internal/mcp"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const (
	DevelopmentVersion = "dev"
	UnknownRevision    = "unknown"
	UnknownSchema      = "unavailable"
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
	executor stratz.Executor
}

// NewRuntime composes the STRATZ client and static MCP server.
func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	build := options.Build.Normalized()
	executor := options.Executor
	if executor == nil {
		client, err := stratz.New(options.Credential, build.Version, options.Config.Limits)
		if err != nil {
			return nil, fmt.Errorf("construct STRATZ client: %w", err)
		}
		executor = client
	}
	server, err := mcpserver.New(mcpserver.Options{
		Version:       build.Version,
		SchemaVersion: build.SchemaVersion,
		Config:        options.Config,
		Executor:      executor,
		Logger:        options.Logger,
		Now:           options.Now,
		Handlers:      options.Handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("construct MCP server: %w", err)
	}
	return &Runtime{
		server:   server,
		executor: executor,
	}, nil
}

// Executor returns the bounded upstream dependency used by runtime commands.
func (runtime *Runtime) Executor() stratz.Executor {
	return runtime.executor
}

// Server returns the configured MCP adapter.
func (runtime *Runtime) Server() *mcpserver.Server {
	return runtime.server
}

// Serve runs the stdio server. The output wrapper deliberately has a no-op
// close so the process owns stdout for its full lifetime.
func (runtime *Runtime) Serve(
	ctx context.Context,
	stdin io.Reader,
	stdout io.Writer,
) error {
	reader, ok := stdin.(io.ReadCloser)
	if !ok {
		reader = io.NopCloser(stdin)
	}
	return runtime.server.Run(ctx, reader, writerCloser{Writer: stdout})
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
