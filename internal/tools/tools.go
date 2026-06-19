//go:build tools

// Package tools pins build-time generators and milestone dependencies before
// their runtime packages are implemented.
package tools

import (
	_ "github.com/Khan/genqlient/generate"
	_ "github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)
