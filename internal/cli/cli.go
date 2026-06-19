// Package cli implements the stratz-mcp command-line interface.
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/app"
)

const usage = `Usage: stratz-mcp <command>

Commands:
  serve         Run the MCP server over stdio
  doctor        Diagnose configuration and STRATZ connectivity
  schema pull   Fetch and generate local schema artifacts
  cache stats   Show cache statistics
  cache clear   Clear cached data
  version       Print build version information
`

// Run executes the command and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer, info app.BuildInfo) int {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "stratz-mcp %s\n", info.String())
		return 0
	case "serve", "doctor", "schema", "cache":
		fmt.Fprintf(stderr, "stratz-mcp: command %q is not implemented yet\n", strings.Join(args, " "))
		return 2
	default:
		fmt.Fprintf(stderr, "stratz-mcp: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func isHelp(argument string) bool {
	return argument == "help" || argument == "-h" || argument == "--help"
}
