// Package cli implements the stratz-mcp command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/app"
	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/doctor"
	"github.com/aneviaro/stratz-mcp/internal/observability"
	schemalifecycle "github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const usage = `Usage: stratz-mcp <command>

Commands:
  serve         Run the MCP server over stdio
  doctor        Diagnose configuration and STRATZ connectivity
  schema pull   Fetch and generate local schema artifacts
  cache stats   Show cache statistics
  cache clear   Clear cached data
  version       Print build version information

Global options:
  --config <path>       Load an explicit strict YAML configuration
  --env-file <path>     Load STRATZ_API_TOKEN from an explicit dotenv file
  --token-file <path>   Load the token from a read-only file
  --log-level <level>   Set error, warn, info, or debug logging
  --log-format <format> Set text or json logging
`

type Dependencies struct {
	Environ      []string
	UserCacheDir func() (string, error)
	Context      context.Context
	Stdin        io.Reader
	Executor     stratz.Executor
	Now          func() time.Time
}

// Run executes the command and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer, info app.BuildInfo) int {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	return RunWithDependencies(args, stdout, stderr, info, Dependencies{
		Environ:      os.Environ(),
		UserCacheDir: os.UserCacheDir,
		Context:      ctx,
		Stdin:        os.Stdin,
	})
}

// RunWithDependencies provides deterministic process inputs for tests.
func RunWithDependencies(
	args []string,
	stdout, stderr io.Writer,
	info app.BuildInfo,
	dependencies Dependencies,
) int {
	if dependencies.Context == nil {
		dependencies.Context = context.Background()
	}
	if dependencies.Stdin == nil {
		dependencies.Stdin = strings.NewReader("")
	}
	options, commandArgs, err := config.ParseCLI(args)
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: %v\n", err)
		return 2
	}
	if len(commandArgs) == 0 || isHelp(commandArgs[0]) {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch commandArgs[0] {
	case "version", "--version":
		if len(commandArgs) != 1 {
			fmt.Fprintln(stderr, "stratz-mcp: version does not accept arguments")
			return 2
		}
		fmt.Fprintf(stdout, "stratz-mcp %s\n", info.String())
		return 0
	case "doctor":
		if len(commandArgs) != 1 {
			fmt.Fprintln(stderr, "stratz-mcp: doctor does not accept positional arguments")
			return 2
		}
		return runDoctor(options, dependencies, stdout, stderr, info)
	case "serve":
		if len(commandArgs) != 1 {
			fmt.Fprintln(stderr, "stratz-mcp: serve does not accept positional arguments")
			return 2
		}
		loaded, credential, ok := loadRuntime(options, dependencies, stderr)
		if !ok {
			return 2
		}
		logger, err := observability.Logger(
			stderr,
			loaded.Config.Logging,
			credential.Token,
		)
		if err != nil {
			fmt.Fprintf(stderr, "stratz-mcp: configure logging: %v\n", err)
			return 2
		}
		runtime, err := app.NewRuntime(app.RuntimeOptions{
			Build:      info,
			Config:     loaded.Config,
			Credential: credential,
			Logger:     logger,
			Executor:   dependencies.Executor,
			Now:        dependencies.Now,
		})
		if err != nil {
			logger.Error("runtime initialization failed", "error", err)
			return 2
		}
		err = runtime.Serve(dependencies.Context, dependencies.Stdin, stdout)
		if err == nil || errors.Is(err, context.Canceled) {
			return 0
		}
		logger.Error("MCP server stopped", "error", err)
		return 1
	case "schema":
		if len(commandArgs) != 2 || commandArgs[1] != "pull" {
			fmt.Fprintln(stderr, "stratz-mcp: usage: stratz-mcp schema pull")
			return 2
		}
		return runSchemaPull(options, dependencies, stdout, stderr, info)
	case "cache":
		fmt.Fprintf(stderr, "stratz-mcp: command %q is not implemented yet\n", strings.Join(commandArgs, " "))
		return 2
	default:
		fmt.Fprintf(stderr, "stratz-mcp: unknown command %q\n\n%s", commandArgs[0], usage)
		return 2
	}
}

func runSchemaPull(
	options config.CLIOptions,
	dependencies Dependencies,
	stdout, stderr io.Writer,
	info app.BuildInfo,
) int {
	loaded, credential, ok := loadRuntime(options, dependencies, stderr)
	if !ok {
		return 2
	}
	if strings.TrimSpace(loaded.Config.Cache.Directory) == "" {
		fmt.Fprintln(stderr, "stratz-mcp: schema pull requires a configured cache directory")
		return 2
	}
	logger, err := observability.Logger(stderr, loaded.Config.Logging, credential.Token)
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: configure logging: %v\n", err)
		return 2
	}
	runtime, err := app.NewRuntime(app.RuntimeOptions{
		Build:      info,
		Config:     loaded.Config,
		Credential: credential,
		Logger:     logger,
		Executor:   dependencies.Executor,
		Now:        dependencies.Now,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: runtime initialization error: %v\n", err)
		return 2
	}
	directory := filepath.Join(loaded.Config.Cache.Directory, "schema")
	manifest, err := schemalifecycle.Pull(
		dependencies.Context,
		runtime.Executor(),
		directory,
	)
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: schema pull failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(
		stdout,
		"schema: generated %s in %s (restricted local data; do not publish)\n",
		manifest.SchemaHash,
		directory,
	)
	return 0
}

func isHelp(argument string) bool {
	return argument == "help" || argument == "-h" || argument == "--help"
}

func runDoctor(
	options config.CLIOptions,
	dependencies Dependencies,
	stdout, stderr io.Writer,
	info app.BuildInfo,
) int {
	loaded, credential, ok := loadRuntime(options, dependencies, stderr)
	if !ok {
		return 2
	}
	logger, err := observability.Logger(
		stderr,
		loaded.Config.Logging,
		credential.Token,
	)
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: configure logging: %v\n", err)
		return 2
	}
	runtime, err := app.NewRuntime(app.RuntimeOptions{
		Build:      info,
		Config:     loaded.Config,
		Credential: credential,
		Logger:     logger,
		Executor:   dependencies.Executor,
		Now:        dependencies.Now,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: runtime initialization error: %v\n", err)
		return 2
	}

	fmt.Fprintln(stdout, "configuration: valid")
	fmt.Fprintf(stdout, "credentials: valid (%s source)\n", credential.Source)
	report := doctor.Diagnose(dependencies.Context, doctor.Options{
		Paths: doctor.Paths{
			TokenFile:      loaded.TokenFile,
			EnvFile:        loaded.EnvFile,
			ConfigFile:     loaded.ConfigFile,
			CacheDirectory: loaded.Config.Cache.Directory,
		},
		Config:        loaded.Config,
		Executor:      runtime.Executor(),
		SchemaVersion: info.Normalized().SchemaVersion,
	})
	for _, finding := range report.Findings {
		fmt.Fprintf(
			stdout,
			"%s: %s: %s\n",
			finding.Severity,
			finding.Subject,
			finding.Message,
		)
	}
	if report.HasErrors() {
		return 2
	}
	return 0
}

func loadRuntime(
	options config.CLIOptions,
	dependencies Dependencies,
	stderr io.Writer,
) (config.Loaded, auth.Credential, bool) {
	loaded, err := config.Load(config.LoadOptions{
		CLI:          options,
		Environ:      dependencies.Environ,
		UserCacheDir: dependencies.UserCacheDir,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: configuration error: %v\n", err)
		return config.Loaded{}, auth.Credential{}, false
	}
	credential, err := auth.Load(auth.LoadOptions{
		Environment: loaded.Environment,
		TokenFile:   loaded.TokenFile,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stratz-mcp: credential error: %v\n", err)
		return config.Loaded{}, auth.Credential{}, false
	}
	return loaded, credential, true
}
