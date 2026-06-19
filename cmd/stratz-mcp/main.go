package main

import (
	"os"

	"github.com/aneviaro/stratz-mcp/internal/app"
	"github.com/aneviaro/stratz-mcp/internal/cli"
)

var (
	version       = "dev"
	revision      = "unknown"
	schemaVersion = "unavailable"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, buildInfo()))
}

func buildInfo() app.BuildInfo {
	return app.BuildInfo{
		Version:       version,
		Revision:      revision,
		SchemaVersion: schemaVersion,
	}.Normalized()
}
