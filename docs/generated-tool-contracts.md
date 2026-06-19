---
Created: 2026-06-19
Purpose: Generated reference for the public STRATZ MCP tool contracts.
Status: Generated from docs/tool-contracts.json; do not edit manually
---

# Generated STRATZ MCP tool contracts

- Contract version: `1.0.0-draft.2`
- MCP protocol version: `2025-11-25`
- JSON Schema dialect: `https://json-schema.org/draft/2020-12/schema`
- Tool count: `15`

| Tool | Description | Required input fields |
|---|---|---|
| `stratz_batch_get_heroes` | Get up to 25 heroes atomically; any item failure or ambiguity fails the entire batch. | `heroes` |
| `stratz_batch_get_matches` | Get up to 25 matches atomically; any item failure fails the entire batch. | `match_ids` |
| `stratz_batch_get_players` | Get up to 25 players atomically; any item failure fails the entire batch. | `player_ids` |
| `stratz_execute_graphql` | Execute one guarded GraphQL query against an approved STRATZ root field using JSON-compatible variables. | `query` |
| `stratz_get_constants` | Get one explicit class of STRATZ/Dota reference constants, or all classes when requested. | `type` |
| `stratz_get_hero` | Get normalized hero reference data by numeric ID, exact localized name, or canonical slug. | `hero` |
| `stratz_get_hero_stats` | Get bounded aggregate hero statistics for a date, patch, rank, role, and lane window. | `hero` |
| `stratz_get_league` | Get normalized league metadata by exact league ID. | `league_id` |
| `stratz_get_match` | Get a normalized Dota match with detail-level controlled timelines and replay-derived events. | `match_id` |
| `stratz_get_player` | Get a normalized STRATZ player profile by account ID, SteamID64, or STRATZ profile URL. | `player_id` |
| `stratz_list_league_matches` | List matches for a league with bounded date/patch filters and an authenticated opaque cursor. | `league_id` |
| `stratz_list_leagues` | Search and list STRATZ leagues with bounded filters and an authenticated opaque cursor. | None |
| `stratz_list_live_matches` | List current live matches with bounded filters, sorting, and a short-lived authenticated cursor. | None |
| `stratz_list_player_matches` | List normalized matches for a player using bounded filters and an authenticated opaque cursor. | `player_id` |
| `stratz_server_info` | Return server, protocol, schema, cache, limit, and upstream connectivity information without secrets. | None |

Dereferenced input and output schemas, validating examples, and JSON-RPC fixtures are embedded from `internal/contracts/generated`.
