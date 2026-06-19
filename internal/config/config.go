// Package config loads strict, explicit STRATZ MCP configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aneviaro/stratz-mcp/internal/securefile"
	"go.yaml.in/yaml/v3"
)

const (
	maxConfigBytes = 1 << 20
	maxDotenvBytes = 64 << 10
)

// Config contains non-secret runtime configuration. API credentials are
// intentionally loaded by internal/auth and cannot be represented in YAML.
type Config struct {
	Limits                  LimitsConfig   `yaml:"limits"`
	Cache                   CacheConfig    `yaml:"cache"`
	Logging                 LoggingConfig  `yaml:"logging"`
	Features                FeaturesConfig `yaml:"features"`
	DefaultPlayerIdentifier string         `yaml:"default_player_identifier"`
}

type LimitsConfig struct {
	UpstreamTimeout         time.Duration `yaml:"upstream_timeout"`
	MaxResponseBytes        int64         `yaml:"max_response_bytes"`
	MaxQueryDocumentBytes   int64         `yaml:"max_query_document_bytes"`
	MaxQueryVariablesBytes  int64         `yaml:"max_query_variables_bytes"`
	MaxQueryVariablesDepth  int           `yaml:"max_query_variables_depth"`
	MaxQueryVariablesNodes  int           `yaml:"max_query_variables_nodes"`
	MaxQueryDepth           int           `yaml:"max_query_depth"`
	MaxQueryAliases         int           `yaml:"max_query_aliases"`
	MaxQueryFields          int           `yaml:"max_query_fields"`
	MaxQueryTopLevelFields  int           `yaml:"max_query_top_level_fields"`
	MaxQueryComplexity      int           `yaml:"max_query_complexity"`
	MaxListPageSize         int           `yaml:"max_list_page_size"`
	MaxNestedListDepth      int           `yaml:"max_nested_list_depth"`
	MaxGraphQLOperations    int           `yaml:"max_graphql_operations"`
	MaxUpstreamRequests     int           `yaml:"max_upstream_requests"`
	MaxBatchSize            int           `yaml:"max_batch_size"`
	MaxIndividualStringSize int64         `yaml:"max_individual_string_bytes"`
}

type CacheConfig struct {
	Enabled               bool          `yaml:"enabled"`
	Directory             string        `yaml:"directory"`
	MaxSizeBytes          int64         `yaml:"max_size_bytes"`
	PublicReferenceTTL    time.Duration `yaml:"public_reference_ttl"`
	PublicReferenceStale  time.Duration `yaml:"public_reference_stale"`
	PublicHistoricalTTL   time.Duration `yaml:"public_historical_ttl"`
	PublicHistoricalStale time.Duration `yaml:"public_historical_stale"`
	ProfileSensitiveTTL   time.Duration `yaml:"profile_sensitive_ttl"`
	ProfileSensitiveStale time.Duration `yaml:"profile_sensitive_stale"`
	PublicRecentTTL       time.Duration `yaml:"public_recent_ttl"`
	PublicRecentStale     time.Duration `yaml:"public_recent_stale"`
	PublicLiveTTL         time.Duration `yaml:"public_live_ttl"`
	PublicLiveStale       time.Duration `yaml:"public_live_stale"`
	RawTTL                time.Duration `yaml:"raw_ttl"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type FeaturesConfig struct {
	RuntimeIntrospection bool `yaml:"runtime_introspection"`
	RawCache             bool `yaml:"raw_cache"`
}

// CLIOptions contains global flags that can override environment and YAML
// configuration. Pointer fields distinguish an absent flag from an explicit
// value.
type CLIOptions struct {
	ConfigFile *string
	EnvFile    *string
	TokenFile  *string
	LogLevel   *string
	LogFormat  *string
}

// LoadOptions supplies testable process inputs.
type LoadOptions struct {
	CLI          CLIOptions
	Environ      []string
	UserCacheDir func() (string, error)
}

// Loaded contains validated non-secret configuration and explicit source
// metadata needed by credential loading and doctor diagnostics.
type Loaded struct {
	Config       Config
	Environment  map[string]string
	ConfigFile   string
	EnvFile      string
	TokenFile    string
	TokenFromEnv bool
}

// Defaults returns the bounded operational defaults from the architecture.
func Defaults(userCacheDir string) Config {
	cacheDirectory := ""
	if strings.TrimSpace(userCacheDir) != "" {
		cacheDirectory = filepath.Join(userCacheDir, "stratz-mcp")
	}
	return Config{
		Limits: LimitsConfig{
			UpstreamTimeout:         20 * time.Second,
			MaxResponseBytes:        5 << 20,
			MaxQueryDocumentBytes:   64 << 10,
			MaxQueryVariablesBytes:  256 << 10,
			MaxQueryVariablesDepth:  16,
			MaxQueryVariablesNodes:  1000,
			MaxQueryDepth:           12,
			MaxQueryAliases:         50,
			MaxQueryFields:          500,
			MaxQueryTopLevelFields:  20,
			MaxQueryComplexity:      1000,
			MaxListPageSize:         100,
			MaxNestedListDepth:      2,
			MaxGraphQLOperations:    1,
			MaxUpstreamRequests:     5,
			MaxBatchSize:            25,
			MaxIndividualStringSize: 64 << 10,
		},
		Cache: CacheConfig{
			Enabled:               true,
			Directory:             cacheDirectory,
			MaxSizeBytes:          512 << 20,
			PublicReferenceTTL:    24 * time.Hour,
			PublicReferenceStale:  24 * time.Hour,
			PublicHistoricalTTL:   6 * time.Hour,
			PublicHistoricalStale: 24 * time.Hour,
			ProfileSensitiveTTL:   15 * time.Minute,
			ProfileSensitiveStale: time.Hour,
			PublicRecentTTL:       5 * time.Minute,
			PublicRecentStale:     15 * time.Minute,
			PublicLiveTTL:         30 * time.Second,
			PublicLiveStale:       2 * time.Minute,
			RawTTL:                5 * time.Minute,
		},
		Logging: LoggingConfig{
			Level:  "error",
			Format: "text",
		},
	}
}

// ParseCLI removes recognized global flags wherever they appear and returns
// the remaining command arguments.
func ParseCLI(args []string) (CLIOptions, []string, error) {
	var options CLIOptions
	remaining := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		argument := args[index]
		name, inlineValue, hasInlineValue := strings.Cut(argument, "=")

		var destination **string
		switch name {
		case "--config":
			destination = &options.ConfigFile
		case "--env-file":
			destination = &options.EnvFile
		case "--token-file":
			destination = &options.TokenFile
		case "--log-level":
			destination = &options.LogLevel
		case "--log-format":
			destination = &options.LogFormat
		default:
			remaining = append(remaining, argument)
			continue
		}

		value := inlineValue
		if !hasInlineValue {
			index++
			if index >= len(args) {
				return CLIOptions{}, nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[index]
		}
		if value == "" {
			return CLIOptions{}, nil, fmt.Errorf("%s requires a non-empty value", name)
		}
		valueCopy := value
		*destination = &valueCopy
	}
	return options, remaining, nil
}

// Load applies YAML, environment, and CLI configuration in increasing
// precedence order. Configuration and dotenv files are never discovered
// implicitly.
func Load(options LoadOptions) (Loaded, error) {
	environment := environmentMap(options.Environ)
	userCacheDir := options.UserCacheDir
	if userCacheDir == nil {
		userCacheDir = os.UserCacheDir
	}
	cacheDirectory, cacheDirectoryError := userCacheDir()

	loaded := Loaded{
		Config:      Defaults(cacheDirectory),
		Environment: environment,
		ConfigFile:  selected(options.CLI.ConfigFile, environment["STRATZ_CONFIG_FILE"]),
		EnvFile:     selected(options.CLI.EnvFile, environment["STRATZ_ENV_FILE"]),
		TokenFile:   selected(options.CLI.TokenFile, environment["STRATZ_API_TOKEN_FILE"]),
	}

	if loaded.ConfigFile != "" {
		if err := decodeYAMLFile(loaded.ConfigFile, &loaded.Config); err != nil {
			return Loaded{}, fmt.Errorf("load explicit YAML configuration: %w", err)
		}
	}

	if loaded.EnvFile != "" {
		dotenvToken, err := loadDotenvToken(loaded.EnvFile)
		if err != nil {
			return Loaded{}, fmt.Errorf("load explicit dotenv file: %w", err)
		}
		if environment["STRATZ_API_TOKEN"] != "" {
			return Loaded{}, errors.New("STRATZ_API_TOKEN is set in both the process environment and explicit dotenv file")
		}
		environment["STRATZ_API_TOKEN"] = dotenvToken
		loaded.TokenFromEnv = true
	} else {
		loaded.TokenFromEnv = environment["STRATZ_API_TOKEN"] != ""
	}

	if err := applyEnvironment(&loaded.Config, environment); err != nil {
		return Loaded{}, err
	}
	applyCLI(&loaded.Config, options.CLI)
	if cacheDirectoryError != nil &&
		loaded.Config.Cache.Enabled &&
		strings.TrimSpace(loaded.Config.Cache.Directory) == "" {
		return Loaded{}, fmt.Errorf("determine user cache directory: %w", cacheDirectoryError)
	}
	if err := loaded.Config.Validate(); err != nil {
		return Loaded{}, err
	}
	return loaded, nil
}

// Validate rejects invalid or unsafe configuration values.
func (config Config) Validate() error {
	defaults := Defaults(".")
	checks := []struct {
		name       string
		value, max int64
	}{
		{"limits.upstream_timeout", int64(config.Limits.UpstreamTimeout), int64(2 * time.Minute)},
		{"limits.max_response_bytes", config.Limits.MaxResponseBytes, defaults.Limits.MaxResponseBytes},
		{"limits.max_query_document_bytes", config.Limits.MaxQueryDocumentBytes, defaults.Limits.MaxQueryDocumentBytes},
		{"limits.max_query_variables_bytes", config.Limits.MaxQueryVariablesBytes, defaults.Limits.MaxQueryVariablesBytes},
		{"limits.max_query_variables_depth", int64(config.Limits.MaxQueryVariablesDepth), int64(defaults.Limits.MaxQueryVariablesDepth)},
		{"limits.max_query_variables_nodes", int64(config.Limits.MaxQueryVariablesNodes), int64(defaults.Limits.MaxQueryVariablesNodes)},
		{"limits.max_query_depth", int64(config.Limits.MaxQueryDepth), int64(defaults.Limits.MaxQueryDepth)},
		{"limits.max_query_aliases", int64(config.Limits.MaxQueryAliases), int64(defaults.Limits.MaxQueryAliases)},
		{"limits.max_query_fields", int64(config.Limits.MaxQueryFields), int64(defaults.Limits.MaxQueryFields)},
		{"limits.max_query_top_level_fields", int64(config.Limits.MaxQueryTopLevelFields), int64(defaults.Limits.MaxQueryTopLevelFields)},
		{"limits.max_query_complexity", int64(config.Limits.MaxQueryComplexity), int64(defaults.Limits.MaxQueryComplexity)},
		{"limits.max_list_page_size", int64(config.Limits.MaxListPageSize), int64(defaults.Limits.MaxListPageSize)},
		{"limits.max_nested_list_depth", int64(config.Limits.MaxNestedListDepth), int64(defaults.Limits.MaxNestedListDepth)},
		{"limits.max_graphql_operations", int64(config.Limits.MaxGraphQLOperations), int64(defaults.Limits.MaxGraphQLOperations)},
		{"limits.max_upstream_requests", int64(config.Limits.MaxUpstreamRequests), int64(defaults.Limits.MaxUpstreamRequests)},
		{"limits.max_batch_size", int64(config.Limits.MaxBatchSize), int64(defaults.Limits.MaxBatchSize)},
		{"limits.max_individual_string_bytes", config.Limits.MaxIndividualStringSize, defaults.Limits.MaxIndividualStringSize},
		{"cache.max_size_bytes", config.Cache.MaxSizeBytes, defaults.Cache.MaxSizeBytes},
	}
	for _, check := range checks {
		if check.value <= 0 || check.value > check.max {
			return fmt.Errorf("%s must be between 1 and %d", check.name, check.max)
		}
	}

	if config.Cache.Enabled && strings.TrimSpace(config.Cache.Directory) == "" {
		return errors.New("cache.directory is required when caching is enabled")
	}
	ttlChecks := []struct {
		name       string
		value, max time.Duration
	}{
		{"cache.public_reference_ttl", config.Cache.PublicReferenceTTL, 24 * time.Hour},
		{"cache.public_reference_stale", config.Cache.PublicReferenceStale, 24 * time.Hour},
		{"cache.public_historical_ttl", config.Cache.PublicHistoricalTTL, 24 * time.Hour},
		{"cache.public_historical_stale", config.Cache.PublicHistoricalStale, 24 * time.Hour},
		{"cache.profile_sensitive_ttl", config.Cache.ProfileSensitiveTTL, time.Hour},
		{"cache.profile_sensitive_stale", config.Cache.ProfileSensitiveStale, time.Hour},
		{"cache.public_recent_ttl", config.Cache.PublicRecentTTL, time.Hour},
		{"cache.public_recent_stale", config.Cache.PublicRecentStale, 15 * time.Minute},
		{"cache.public_live_ttl", config.Cache.PublicLiveTTL, 2 * time.Minute},
		{"cache.public_live_stale", config.Cache.PublicLiveStale, 2 * time.Minute},
		{"cache.raw_ttl", config.Cache.RawTTL, time.Hour},
	}
	for _, check := range ttlChecks {
		if check.value <= 0 || check.value > check.max {
			return fmt.Errorf("%s must be between 1ns and %s", check.name, check.max)
		}
	}

	switch config.Logging.Level {
	case "error", "warn", "info", "debug":
	default:
		return fmt.Errorf("logging.level must be one of error, warn, info, or debug")
	}
	switch config.Logging.Format {
	case "text", "json":
	default:
		return fmt.Errorf("logging.format must be text or json")
	}
	if config.Features.RawCache {
		return errors.New("features.raw_cache cannot be enabled before raw-field cache classification is approved")
	}
	if len(config.DefaultPlayerIdentifier) > 256 {
		return errors.New("default_player_identifier exceeds 256 bytes")
	}
	for _, character := range config.DefaultPlayerIdentifier {
		if character == 0 || unicode.IsControl(character) {
			return errors.New("default_player_identifier contains a prohibited control character")
		}
	}
	return nil
}

func selected(cliValue *string, environmentValue string) string {
	if cliValue != nil {
		return *cliValue
	}
	return environmentValue
}

func environmentMap(environ []string) map[string]string {
	result := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func decodeYAMLFile(path string, destination *Config) error {
	data, err := readExplicitFile(path, maxConfigBytes)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple YAML documents are not allowed")
		}
		return err
	}
	return nil
}

func loadDotenvToken(path string) (string, error) {
	data, err := readExplicitFile(path, maxDotenvBytes)
	if err != nil {
		return "", err
	}

	var token *string
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return "", fmt.Errorf("invalid dotenv assignment on line %d", lineNumber+1)
		}
		if strings.TrimSpace(key) != "STRATZ_API_TOKEN" {
			continue
		}
		if token != nil {
			return "", errors.New("dotenv file defines STRATZ_API_TOKEN more than once")
		}
		parsed, err := parseDotenvValue(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("invalid STRATZ_API_TOKEN dotenv value on line %d", lineNumber+1)
		}
		token = &parsed
	}
	if token == nil || *token == "" {
		return "", errors.New("dotenv file does not define a non-empty STRATZ_API_TOKEN")
	}
	return *token, nil
}

func parseDotenvValue(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if strings.HasPrefix(value, `"`) {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return parsed, nil
	}
	return value, nil
}

func readExplicitFile(path string, maximum int64) ([]byte, error) {
	file, err := securefile.OpenReadOnly(path)
	if err != nil {
		return nil, fmt.Errorf("explicit file is invalid or unusable: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, errors.New("explicit file cannot be read")
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("explicit file exceeds %d bytes", maximum)
	}
	return data, nil
}

func applyCLI(config *Config, options CLIOptions) {
	if options.LogLevel != nil {
		config.Logging.Level = *options.LogLevel
	}
	if options.LogFormat != nil {
		config.Logging.Format = *options.LogFormat
	}
}

func applyEnvironment(config *Config, environment map[string]string) error {
	stringValues := map[string]*string{
		"STRATZ_CACHE_DIR":                 &config.Cache.Directory,
		"STRATZ_LOG_LEVEL":                 &config.Logging.Level,
		"STRATZ_LOG_FORMAT":                &config.Logging.Format,
		"STRATZ_DEFAULT_PLAYER_IDENTIFIER": &config.DefaultPlayerIdentifier,
	}
	for name, destination := range stringValues {
		if value, ok := environment[name]; ok && value != "" {
			*destination = value
		}
	}

	boolValues := map[string]*bool{
		"STRATZ_CACHE_ENABLED":         &config.Cache.Enabled,
		"STRATZ_RUNTIME_INTROSPECTION": &config.Features.RuntimeIntrospection,
		"STRATZ_RAW_CACHE":             &config.Features.RawCache,
	}
	for name, destination := range boolValues {
		if value, ok := environment[name]; ok && value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s must be a boolean", name)
			}
			*destination = parsed
		}
	}

	int64Values := map[string]*int64{
		"STRATZ_MAX_RESPONSE_BYTES":          &config.Limits.MaxResponseBytes,
		"STRATZ_MAX_QUERY_DOCUMENT_BYTES":    &config.Limits.MaxQueryDocumentBytes,
		"STRATZ_MAX_QUERY_VARIABLES_BYTES":   &config.Limits.MaxQueryVariablesBytes,
		"STRATZ_MAX_INDIVIDUAL_STRING_BYTES": &config.Limits.MaxIndividualStringSize,
		"STRATZ_CACHE_MAX_SIZE_BYTES":        &config.Cache.MaxSizeBytes,
	}
	for name, destination := range int64Values {
		if value, ok := environment[name]; ok && value != "" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("%s must be an integer", name)
			}
			*destination = parsed
		}
	}

	intValues := map[string]*int{
		"STRATZ_MAX_QUERY_VARIABLES_DEPTH":  &config.Limits.MaxQueryVariablesDepth,
		"STRATZ_MAX_QUERY_VARIABLES_NODES":  &config.Limits.MaxQueryVariablesNodes,
		"STRATZ_MAX_QUERY_DEPTH":            &config.Limits.MaxQueryDepth,
		"STRATZ_MAX_QUERY_ALIASES":          &config.Limits.MaxQueryAliases,
		"STRATZ_MAX_QUERY_FIELDS":           &config.Limits.MaxQueryFields,
		"STRATZ_MAX_QUERY_TOP_LEVEL_FIELDS": &config.Limits.MaxQueryTopLevelFields,
		"STRATZ_MAX_QUERY_COMPLEXITY":       &config.Limits.MaxQueryComplexity,
		"STRATZ_MAX_LIST_PAGE_SIZE":         &config.Limits.MaxListPageSize,
		"STRATZ_MAX_NESTED_LIST_DEPTH":      &config.Limits.MaxNestedListDepth,
		"STRATZ_MAX_GRAPHQL_OPERATIONS":     &config.Limits.MaxGraphQLOperations,
		"STRATZ_REQUEST_BUDGET":             &config.Limits.MaxUpstreamRequests,
		"STRATZ_MAX_BATCH_SIZE":             &config.Limits.MaxBatchSize,
	}
	for name, destination := range intValues {
		if value, ok := environment[name]; ok && value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s must be an integer", name)
			}
			*destination = parsed
		}
	}

	durationValues := map[string]*time.Duration{
		"STRATZ_UPSTREAM_TIMEOUT":              &config.Limits.UpstreamTimeout,
		"STRATZ_CACHE_PUBLIC_REFERENCE_TTL":    &config.Cache.PublicReferenceTTL,
		"STRATZ_CACHE_PUBLIC_REFERENCE_STALE":  &config.Cache.PublicReferenceStale,
		"STRATZ_CACHE_PUBLIC_HISTORICAL_TTL":   &config.Cache.PublicHistoricalTTL,
		"STRATZ_CACHE_PUBLIC_HISTORICAL_STALE": &config.Cache.PublicHistoricalStale,
		"STRATZ_CACHE_PROFILE_SENSITIVE_TTL":   &config.Cache.ProfileSensitiveTTL,
		"STRATZ_CACHE_PROFILE_SENSITIVE_STALE": &config.Cache.ProfileSensitiveStale,
		"STRATZ_CACHE_PUBLIC_RECENT_TTL":       &config.Cache.PublicRecentTTL,
		"STRATZ_CACHE_PUBLIC_RECENT_STALE":     &config.Cache.PublicRecentStale,
		"STRATZ_CACHE_PUBLIC_LIVE_TTL":         &config.Cache.PublicLiveTTL,
		"STRATZ_CACHE_PUBLIC_LIVE_STALE":       &config.Cache.PublicLiveStale,
		"STRATZ_CACHE_RAW_TTL":                 &config.Cache.RawTTL,
	}
	for name, destination := range durationValues {
		if value, ok := environment[name]; ok && value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s must be a duration", name)
			}
			*destination = parsed
		}
	}
	return nil
}
