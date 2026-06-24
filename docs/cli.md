---
Created: 2026-06-21
Purpose: Document the public STRATZ MCP command-line interface.
Status: Current
---

# CLI reference

Global options may appear before or after the command: `--config`, `--env-file`, `--token-file`, `--log-level`, and `--log-format`.

`STRATZ_CONFIG_FILE`, `STRATZ_ENV_FILE`, and `STRATZ_API_TOKEN_FILE` select the same explicit files when the corresponding CLI flag is absent. No configuration or dotenv file is auto-discovered.

| Command | Behavior |
| --- | --- |
| `serve` | Run the MCP 2025-11-25 stdio server until EOF or cancellation. |
| `doctor` | Check configuration, pre-existing file permissions, cache health, schema availability, and upstream connectivity. |
| `schema pull` | Fetch authenticated introspection and constants, then atomically replace the restricted local schema bundle. |
| `cache stats` | Print cache status, format, entry, namespace, and byte counts. |
| `cache clear [--domain NAME] [--current-token]` | Transactionally remove all entries or the selected domain/token namespace. The filters may be combined; `--current-token` requires a credential. |
| `version` | Print version, revision, and build schema metadata. |

Exit status is `0` for success, `1` for an operational failure, and `2` for invalid arguments or startup configuration.
