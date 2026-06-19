# Troubleshooting

- Run `stratz-mcp doctor` first. It reports invalid credentials, permissions, connectivity, schema drift, cache fallback, and release-clearance state.
- If the MCP client cannot start the server, use an absolute executable path and confirm the client passes `serve`.
- If stdout contains non-JSON text, remove shell wrappers that print banners. Server diagnostics belong on stderr.
- For authentication failures, configure exactly one token source and ensure token files contain one bounded line with restrictive permissions.
- For cache failures, verify the directory is private, writable, and not a symlink. Use `cache stats`; clear only after preserving any needed diagnostics.
- `DATA_NOT_READY` means STRATZ has not parsed enough upstream data yet. Retry later; increasing local limits does not fix it.
- Partial or stale results include warnings and provenance. Do not present them as complete snapshots.
- Docker should run with `-i`, a read-only root filesystem, and a writable `/cache` volume.
