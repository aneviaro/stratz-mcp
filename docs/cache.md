# Cache

The optional SQLite cache defaults to the platform user cache directory under `stratz-mcp`. It uses WAL mode, bounded stale windows, token namespaces, Zstandard compression, and LRU eviction. Raw GraphQL caching remains disabled pending field-classification approval.

Commands:

```sh
stratz-mcp cache stats
stratz-mcp cache clear
```

Use `STRATZ_CACHE_ENABLED=false` to disable caching or set `STRATZ_CACHE_DIRECTORY` to an explicit private directory. Docker deployments should mount `/cache` as the sole writable volume. Cache initialization or operation failures degrade to process-local no-cache behavior; `doctor` reports permission and corruption findings.
