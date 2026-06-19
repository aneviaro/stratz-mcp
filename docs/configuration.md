# Configuration

`stratz-mcp` applies configuration in this order: command-line flags, environment variables, an explicitly selected strict YAML file, then bounded defaults. YAML rejects unknown keys. Dotenv files are loaded only with `--env-file` or `STRATZ_ENV_FILE`.

Credentials must come from exactly one source: `STRATZ_API_TOKEN`, an explicit dotenv file, or `--token-file`/`STRATZ_API_TOKEN_FILE`. Token files must be regular, non-symlink files with restrictive permissions and a bounded single-line value.

Use `stratz-mcp doctor` to validate configuration, permissions, cache initialization, connectivity, schema status, and release clearance. Use `--log-level error|warn|info|debug` and `--log-format text|json`; logs are written to stderr with centralized credential/header redaction.

The strict YAML structure is `limits`, `cache`, `logging`, `features`, and `default_player_identifier`. Defaults and every supported environment key are defined in `internal/config/config.go`. Raw-query caching and runtime introspection are disabled unless explicitly enabled and policy permits them.
