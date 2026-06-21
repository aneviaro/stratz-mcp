# Troubleshooting

- Run `stratz-mcp doctor` first. It reports invalid credentials, permissions, connectivity, local schema availability, cache fallback, and release-clearance state.
- If the MCP client cannot start the server, use an absolute executable path and confirm the client passes `serve`.
- If stdout contains non-JSON text, remove shell wrappers that print banners. Server diagnostics belong on stderr.
- For authentication failures, configure exactly one token source and ensure token files are regular, non-symlink files containing one bounded line. Restrictive permissions are strongly recommended and reported by `doctor`.
- For cache failures, verify the directory is private, writable, and not a symlink. Use `cache stats`; clear only after preserving any needed diagnostics.
- `DATA_NOT_READY` means STRATZ has not parsed enough upstream data yet. Retry later; increasing local limits does not fix it.
- Partial or stale results include warnings and provenance. Do not present them as complete snapshots.
- Docker should run with `-i`, a read-only root filesystem, and a writable `/cache` volume.

## Request limits, retries, and pagination

- One MCP call may spend at most five upstream attempts. Eligible rate-limit, timeout, network, and temporary-server failures may be retried at most twice after the initial attempt, within the same deadline and budget.
- Permanent authentication, policy, validation, and not-found failures are not retried.
- Cursors are authenticated and bound to the tool, filters, page size, active token, schema version, and operation version. Changing any binding invalidates the cursor.
- Cursor lifetimes are 5 minutes for live listings, 1 hour for recent listings, and 24 hours for historical league listings.
- A cursor preserves traversal state, not an upstream snapshot. Live and recently changing results may shift between pages.
- If local schema metadata is unavailable or stale, run `stratz-mcp schema pull` and restart the server. Development-time schema drift is checked by generation and schema tests, not by `doctor`.
