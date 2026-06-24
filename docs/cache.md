# Cache

The SQLite response cache defaults to the platform user cache directory under `stratz-mcp`. Curated tools read it before calling STRATZ, asynchronously populate successful eligible responses, honor `fresh`, and use stale data only after an upstream failure. `include_raw` bypasses cache reads and writes. Raw GraphQL caching remains disabled pending field-classification approval.

Commands:

```sh
stratz-mcp cache stats
stratz-mcp cache clear
stratz-mcp cache clear --domain matches
stratz-mcp cache clear --current-token
```

| Class | TTL | Stale window |
| --- | ---: | ---: |
| Public reference | 24h | 24h |
| Public historical | 6h | 24h |
| Profile-sensitive | 15m | 1h |
| Public recent | 5m | 15m |
| Public live | 30s | 2m |

Keys include the token-derived namespace, operation, normalized arguments, detail level, schema version, and cache class. Token rotation therefore starts a separate namespace. Payloads of at least 4 KiB use Zstandard compression; the default size ceiling is 512 MiB with LRU eviction.

Use `STRATZ_CACHE_ENABLED=false` to disable caching or `STRATZ_CACHE_DIR` to select a private directory. Directories use mode `0700`; the database and WAL/SHM files use `0600` on POSIX. Docker deployments should mount `/cache` as the writable volume. Cache initialization or operation failures degrade to no-cache behavior and are reported by `doctor`.

With the server stopped, deleting `cache.db`, `cache.db-wal`, and `cache.db-shm` is the definitive manual purge.

## Future direction

Caching currently lives only at the MCP envelope boundary (`cachedToolHandler`), so domain-internal fetches (hero constants, batched lookups) bypass it. A planned shift moves the boundary into the domain services so normalized data and upstream payloads are cached once and reused across calls.
