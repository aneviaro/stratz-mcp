# Cache

The SQLite response cache is the application data cache for STRATZ-derived tool results. It is separate from MCP response cache hints such as `ttlMs` and `cacheScope` on discovery, catalog, or resource-read responses. MCP hints tell clients how fresh a protocol response is; they do not set SQLite TTLs, stale windows, token namespaces, or eviction behavior.

The SQLite response cache defaults to the platform user cache directory under `stratz-mcp`. Curated tools read it before calling STRATZ, asynchronously populate successful eligible responses, honor `fresh`, and use stale data only after an upstream failure. `include_raw` bypasses cache reads and writes. Raw GraphQL caching remains disabled pending field-classification approval.

Cache classification is request-aware: it is selected only after schema validation from the query tool and its mode/options, never from the consolidated tool name alone.

| Request | Class | TTL | Stale window |
| --- | --- | ---: | ---: |
| `stratz_query_constants` (`types` or `typed_selectors`) | Public reference | 24h | 24h |
| `stratz_query_heroes` (`exact` or `search`) without statistics | Public reference | 24h | 24h |
| `stratz_query_heroes` with `include_statistics: true` | Public recent | 5m | 15m |
| `stratz_query_leagues` (`exact`) | Public reference | 24h | 24h |
| `stratz_query_leagues` (`search`) | Public recent | 5m | 15m |
| `stratz_query_players` (`exact`) | Profile-sensitive | 15m | 1h |
| `stratz_query_matches` (`exact` or `league_history`) | Public recent | 5m | 15m |
| `stratz_query_matches` (`player_history`) | Profile-sensitive | 15m | 1h |
| `stratz_query_matches` (`live`) | Public live | 30s | 2m |

An unknown or mixed future request fails closed rather than receiving a broader cache class. `fresh` bypasses reads and stale fallback; cache failures degrade to no-cache behavior.

Commands:

```sh
stratz-mcp cache stats
stratz-mcp cache clear
stratz-mcp cache clear --domain matches
stratz-mcp cache clear --current-token
```

Keys include the token-derived namespace, operation, normalized arguments, mode, detail level, schema version, and cache class. Token rotation therefore starts a separate namespace, and the v2 tool cutover cold-starts cache keys rather than reusing v1 entries. Payloads of at least 4 KiB use Zstandard compression; the default size ceiling is 512 MiB with LRU eviction.

Use `STRATZ_CACHE_ENABLED=false` to disable caching or `STRATZ_CACHE_DIR` to select a private directory. Directories use mode `0700`; the database and WAL/SHM files use `0600` on POSIX. Docker deployments should mount `/cache` as the writable volume. Cache initialization or operation failures degrade to no-cache behavior and are reported by `doctor`.

With the server stopped, deleting `cache.db`, `cache.db-wal`, and `cache.db-shm` is the definitive manual purge.

MCP protocol cache hints are fixed by response type: `server/discover` is public for 1 hour, tool/resource/prompt catalog lists are public for 5 minutes, and resource reads are private with `ttlMs: 0` because local restricted files may change outside the running client. These hints do not authorize serving stale STRATZ data from SQLite.

## Future direction

Caching currently lives only at the MCP envelope boundary (`cachedToolHandler`), so domain-internal fetches (hero constants, batched lookups) bypass it. A planned shift moves the boundary into the domain services so normalized data and upstream payloads are cached once and reused across calls.
