package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
logging:
  level: info
  format: json
cache:
  enabled: false
`), 0o600); err != nil {
		t.Fatal(err)
	}
	level := "debug"
	loaded, err := Load(LoadOptions{
		CLI: CLIOptions{
			ConfigFile: &configPath,
			LogLevel:   &level,
		},
		Environ: []string{
			"STRATZ_LOG_LEVEL=warn",
			"STRATZ_LOG_FORMAT=text",
		},
		UserCacheDir: func() (string, error) { return directory, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Logging.Level != "debug" {
		t.Fatalf("log level = %q, want CLI value debug", loaded.Config.Logging.Level)
	}
	if loaded.Config.Logging.Format != "text" {
		t.Fatalf("log format = %q, want environment value text", loaded.Config.Logging.Format)
	}
	if loaded.Config.Cache.Enabled {
		t.Fatal("cache enabled did not use YAML value")
	}
	if loaded.Config.Limits.UpstreamTimeout != 20*time.Second {
		t.Fatal("built-in default was not retained")
	}
}

func TestLoadAppliesPublicEnvironmentSettings(t *testing.T) {
	directory := t.TempDir()
	loaded, err := Load(LoadOptions{
		Environ: []string{
			"STRATZ_CACHE_DIR=" + filepath.Join(directory, "cache"),
			"STRATZ_LOG_LEVEL=info",
			"STRATZ_LOG_FORMAT=json",
			"STRATZ_DEFAULT_PLAYER_IDENTIFIER=123",
			"STRATZ_CACHE_ENABLED=false",
			"STRATZ_RUNTIME_INTROSPECTION=true",
			"STRATZ_RAW_CACHE=false",
			"STRATZ_MAX_RESPONSE_BYTES=1024",
			"STRATZ_MAX_QUERY_DOCUMENT_BYTES=2048",
			"STRATZ_MAX_QUERY_VARIABLES_BYTES=4096",
			"STRATZ_MAX_INDIVIDUAL_STRING_BYTES=512",
			"STRATZ_CACHE_MAX_SIZE_BYTES=8192",
			"STRATZ_MAX_QUERY_VARIABLES_DEPTH=3",
			"STRATZ_MAX_QUERY_VARIABLES_NODES=4",
			"STRATZ_MAX_QUERY_DEPTH=5",
			"STRATZ_MAX_QUERY_ALIASES=6",
			"STRATZ_MAX_QUERY_FIELDS=7",
			"STRATZ_MAX_QUERY_TOP_LEVEL_FIELDS=8",
			"STRATZ_MAX_QUERY_COMPLEXITY=9",
			"STRATZ_MAX_LIST_PAGE_SIZE=10",
			"STRATZ_MAX_NESTED_LIST_DEPTH=1",
			"STRATZ_MAX_GRAPHQL_OPERATIONS=1",
			"STRATZ_REQUEST_BUDGET=2",
			"STRATZ_MAX_BATCH_SIZE=3",
			"STRATZ_UPSTREAM_TIMEOUT=30s",
			"STRATZ_CACHE_PUBLIC_REFERENCE_TTL=1h",
			"STRATZ_CACHE_PUBLIC_REFERENCE_STALE=2h",
			"STRATZ_CACHE_PUBLIC_HISTORICAL_TTL=3h",
			"STRATZ_CACHE_PUBLIC_HISTORICAL_STALE=4h",
			"STRATZ_CACHE_PROFILE_SENSITIVE_TTL=5m",
			"STRATZ_CACHE_PROFILE_SENSITIVE_STALE=6m",
			"STRATZ_CACHE_PUBLIC_RECENT_TTL=7m",
			"STRATZ_CACHE_PUBLIC_RECENT_STALE=8m",
			"STRATZ_CACHE_PUBLIC_LIVE_TTL=9s",
			"STRATZ_CACHE_PUBLIC_LIVE_STALE=10s",
			"STRATZ_CACHE_RAW_TTL=11m",
		},
		UserCacheDir: func() (string, error) { return directory, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Config
	if cfg.Limits.MaxResponseBytes != 1024 ||
		cfg.Limits.MaxQueryDocumentBytes != 2048 ||
		cfg.Limits.MaxQueryVariablesBytes != 4096 ||
		cfg.Limits.MaxIndividualStringSize != 512 ||
		cfg.Limits.MaxUpstreamRequests != 2 ||
		cfg.Limits.UpstreamTimeout != 30*time.Second ||
		cfg.Cache.MaxSizeBytes != 8192 ||
		cfg.Cache.PublicHistoricalTTL != 3*time.Hour ||
		cfg.Cache.PublicLiveTTL != 9*time.Second ||
		cfg.Logging.Level != "info" ||
		cfg.Logging.Format != "json" ||
		!cfg.Features.RuntimeIntrospection ||
		cfg.DefaultPlayerIdentifier != "123" {
		t.Fatalf("environment configuration was not applied: %#v", cfg)
	}
}

func TestLoadRejectsInvalidEnvironmentValues(t *testing.T) {
	for _, environment := range []string{
		"STRATZ_CACHE_ENABLED=maybe",
		"STRATZ_MAX_QUERY_DEPTH=not-an-integer",
		"STRATZ_UPSTREAM_TIMEOUT=tomorrow",
	} {
		t.Run(environment, func(t *testing.T) {
			_, err := Load(LoadOptions{
				Environ:      []string{environment},
				UserCacheDir: func() (string, error) { return t.TempDir(), nil },
			})
			if err == nil {
				t.Fatal("invalid environment value was accepted")
			}
		})
	}
}

func TestLoadRejectsUnknownYAMLKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("logging:\n  leve1: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(LoadOptions{
		CLI:          CLIOptions{ConfigFile: &path},
		UserCacheDir: func() (string, error) { return t.TempDir(), nil },
	})
	if err == nil || !strings.Contains(err.Error(), "leve1") {
		t.Fatalf("unknown YAML key error = %v", err)
	}
}

func TestLoadUsesOnlyExplicitFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("STRATZ_API_TOKEN=implicit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("logging:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	loaded, err := Load(LoadOptions{
		UserCacheDir: func() (string, error) { return directory, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Environment["STRATZ_API_TOKEN"] != "" {
		t.Fatal("implicitly discovered .env token")
	}
	if loaded.Config.Logging.Level != "error" {
		t.Fatal("implicitly discovered YAML configuration")
	}
}

func TestLoadExplicitDotenv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(path, []byte("OTHER=value\nexport STRATZ_API_TOKEN='fixture-token'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(LoadOptions{
		CLI:          CLIOptions{EnvFile: &path},
		UserCacheDir: func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Environment["STRATZ_API_TOKEN"]; got != "fixture-token" {
		t.Fatalf("dotenv token = %q", got)
	}
	if !loaded.TokenFromEnv {
		t.Fatal("dotenv token source was not recorded")
	}
}

func TestLoadRejectsDuplicateProcessAndDotenvToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(path, []byte("STRATZ_API_TOKEN=dotenv-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(LoadOptions{
		CLI:          CLIOptions{EnvFile: &path},
		Environ:      []string{"STRATZ_API_TOKEN=process-token"},
		UserCacheDir: func() (string, error) { return t.TempDir(), nil },
	})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("duplicate token error = %v", err)
	}
	if err != nil && (strings.Contains(err.Error(), "process-token") || strings.Contains(err.Error(), "dotenv-token")) {
		t.Fatalf("error leaked a token: %v", err)
	}
}

func TestLoadRejectsExplicitFileSymlinks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "config.yaml")
	link := filepath.Join(directory, "config-link.yaml")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Load(LoadOptions{
		CLI:          CLIOptions{ConfigFile: &link},
		UserCacheDir: func() (string, error) { return directory, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestParseCLIRecognizesGlobalFlagsAnywhere(t *testing.T) {
	options, remaining, err := ParseCLI([]string{
		"schema", "pull", "--env-file", "credentials.env",
		"--log-level=debug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.EnvFile == nil || *options.EnvFile != "credentials.env" {
		t.Fatalf("env file = %v", options.EnvFile)
	}
	if options.LogLevel == nil || *options.LogLevel != "debug" {
		t.Fatalf("log level = %v", options.LogLevel)
	}
	if got := strings.Join(remaining, " "); got != "schema pull" {
		t.Fatalf("remaining args = %q", got)
	}
}

func TestValidateRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"request budget above ceiling", func(config *Config) { config.Limits.MaxUpstreamRequests = 6 }},
		{"invalid log level", func(config *Config) { config.Logging.Level = "trace" }},
		{"raw cache before classification", func(config *Config) { config.Features.RawCache = true }},
		{"empty cache directory", func(config *Config) { config.Cache.Directory = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Defaults(t.TempDir())
			test.mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("validation unexpectedly passed")
			}
		})
	}
}
