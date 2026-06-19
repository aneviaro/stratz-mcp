// Package app composes the STRATZ MCP application's runtime dependencies.
package app

import (
	"fmt"
	"strings"
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
