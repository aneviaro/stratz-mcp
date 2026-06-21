# Configuration

`stratz-mcp` applies configuration in this order: command-line flags, environment variables, an explicitly selected strict YAML file, then bounded defaults. YAML rejects unknown keys. Dotenv files are loaded only with `--env-file` or `STRATZ_ENV_FILE`.

Credentials must come from exactly one source: `STRATZ_API_TOKEN`, an explicit dotenv file, or `--token-file`/`STRATZ_API_TOKEN_FILE`. Token files must be regular, non-symlink files with a bounded single-line value. Restrictive POSIX permissions are strongly recommended; `doctor` warns when a token file is group/other accessible.

Use `stratz-mcp doctor` to validate configuration, permissions, cache initialization, connectivity, schema status, and release clearance. Use `--log-level error|warn|info|debug` and `--log-format text|json`; logs are written to stderr with centralized credential/header redaction.

Durations use Go syntax such as `20s`, `15m`, or `24h`. Byte values are decimal integers. Demand-control limits and cache size cannot exceed their defaults. The upstream timeout and cache TTLs have separate maxima.

## Configuration source selection

| Purpose | CLI | Environment |
| --- | --- | --- |
| Strict YAML configuration | `--config PATH` | `STRATZ_CONFIG_FILE` |
| Explicit dotenv file | `--env-file PATH` | `STRATZ_ENV_FILE` |
| Token file | `--token-file PATH` | `STRATZ_API_TOKEN_FILE` |

CLI selectors override environment selectors. Files are never auto-discovered.

## Bounds

| Setting group | Default | Maximum |
| --- | --- | --- |
| Upstream timeout | `20s` | `2m` |
| Response/query/batch/request limits | Values below | Their listed defaults |
| Cache size | `512 MiB` | `512 MiB` |
| Historical TTL | `6h` | `24h` |
| Profile-sensitive TTL | `15m` | `1h` |
| Recent TTL | `5m` | `1h` |
| Live TTL | `30s` | `2m` |
| Raw TTL (currently disabled) | `5m` | `1h` |

```yaml
limits:
  upstream_timeout: 20s
  max_response_bytes: 5242880
  max_query_document_bytes: 65536
  max_query_variables_bytes: 262144
  max_query_variables_depth: 16
  max_query_variables_nodes: 1000
  max_query_depth: 12
  max_query_aliases: 50
  max_query_fields: 500
  max_query_top_level_fields: 20
  max_query_complexity: 1000
  max_list_page_size: 100
  max_nested_list_depth: 2
  max_graphql_operations: 1
  max_upstream_requests: 5
  max_batch_size: 25
  max_individual_string_bytes: 65536
cache:
  enabled: true
  directory: /private/cache/stratz-mcp
  max_size_bytes: 536870912
  public_reference_ttl: 24h
  public_reference_stale: 24h
  public_historical_ttl: 6h
  public_historical_stale: 24h
  profile_sensitive_ttl: 15m
  profile_sensitive_stale: 1h
  public_recent_ttl: 5m
  public_recent_stale: 15m
  public_live_ttl: 30s
  public_live_stale: 2m
  raw_ttl: 5m
logging:
  level: error
  format: text
features:
  runtime_introspection: false
  raw_cache: false
default_player_identifier: ""
```

Environment names mirror the YAML fields:

- Limits: `STRATZ_UPSTREAM_TIMEOUT`, `STRATZ_MAX_RESPONSE_BYTES`, `STRATZ_MAX_QUERY_DOCUMENT_BYTES`, `STRATZ_MAX_QUERY_VARIABLES_BYTES`, `STRATZ_MAX_QUERY_VARIABLES_DEPTH`, `STRATZ_MAX_QUERY_VARIABLES_NODES`, `STRATZ_MAX_QUERY_DEPTH`, `STRATZ_MAX_QUERY_ALIASES`, `STRATZ_MAX_QUERY_FIELDS`, `STRATZ_MAX_QUERY_TOP_LEVEL_FIELDS`, `STRATZ_MAX_QUERY_COMPLEXITY`, `STRATZ_MAX_LIST_PAGE_SIZE`, `STRATZ_MAX_NESTED_LIST_DEPTH`, `STRATZ_MAX_GRAPHQL_OPERATIONS`, `STRATZ_REQUEST_BUDGET`, `STRATZ_MAX_BATCH_SIZE`, and `STRATZ_MAX_INDIVIDUAL_STRING_BYTES`.
- Cache: `STRATZ_CACHE_ENABLED`, `STRATZ_CACHE_DIR`, `STRATZ_CACHE_MAX_SIZE_BYTES`, and `STRATZ_CACHE_{PUBLIC_REFERENCE,PUBLIC_HISTORICAL,PROFILE_SENSITIVE,PUBLIC_RECENT,PUBLIC_LIVE}_{TTL,STALE}` plus `STRATZ_CACHE_RAW_TTL`.
- Logging/features: `STRATZ_LOG_LEVEL`, `STRATZ_LOG_FORMAT`, `STRATZ_RUNTIME_INTROSPECTION`, `STRATZ_RAW_CACHE`, and `STRATZ_DEFAULT_PLAYER_IDENTIFIER`.

Raw caching cannot currently be enabled: `features.raw_cache: true` and `STRATZ_RAW_CACHE=true` fail validation until field classification is approved. `default_player_identifier` is reserved and currently has no effect.
