---
Created: 2026-06-19
Purpose: Generated reference for the public STRATZ MCP tool contracts.
Status: Generated from docs/tool-contracts.json; do not edit manually
---

# Generated STRATZ MCP tool contracts

- Contract version: `2.0.0-draft.1`
- Preferred MCP protocol version: `2026-07-28`
- Supported MCP protocol versions: `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`
- JSON Schema dialect: `https://json-schema.org/draft/2020-12/schema`
- Tool count: `7`

| Tool | Description | Required input fields |
|---|---|---|
| `stratz_execute_graphql` | Execute one guarded GraphQL query against an approved STRATZ root. | `query` |
| `stratz_query_constants` | Query supported constants by types or typed selectors. | `mode`, `selectors`, `types` |
| `stratz_query_heroes` | Query normalized heroes exactly or by bounded search with optional statistics. | `heroes`, `mode`, `query` |
| `stratz_query_leagues` | Query normalized leagues exactly or by authenticated bounded search. | `league_ids`, `mode` |
| `stratz_query_matches` | Query normalized matches by exact IDs, history filters, or live filters. | `league_id`, `match_ids`, `mode`, `player_id` |
| `stratz_query_players` | Query normalized players by one to 25 exact identifiers. | `mode`, `player_ids` |
| `stratz_server_info` | Return server, protocol, schema, cache, limits, and upstream status. | None |

## Tool details

### `stratz_execute_graphql`

Execute one guarded GraphQL query against an approved STRATZ root.

| Input | Required | Type and constraints |
|---|---:|---|
| `cache` | false | `boolean`; default `false` |
| `cache_ttl_seconds` | false | `integer`; minimum `1`; maximum `3600` |
| `fresh` | false | `boolean`; default `false` |
| `operation_name` | false | `string`; minLength `1`; maxLength `256` |
| `query` | true | `string`; minLength `1`; maxLength `65536` |
| `variables` | false | `object` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_execute_graphql.input.json), [output schema](../internal/contracts/generated/schemas/stratz_execute_graphql.output.json), [examples](../internal/contracts/generated/examples/stratz_execute_graphql.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_execute_graphql.json).

### `stratz_query_constants`

Query supported constants by types or typed selectors.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_query_constants.input.json), [output schema](../internal/contracts/generated/schemas/stratz_query_constants.output.json), [examples](../internal/contracts/generated/examples/stratz_query_constants.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_query_constants.json).

### `stratz_query_heroes`

Query normalized heroes exactly or by bounded search with optional statistics.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_query_heroes.input.json), [output schema](../internal/contracts/generated/schemas/stratz_query_heroes.output.json), [examples](../internal/contracts/generated/examples/stratz_query_heroes.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_query_heroes.json).

### `stratz_query_leagues`

Query normalized leagues exactly or by authenticated bounded search.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_query_leagues.input.json), [output schema](../internal/contracts/generated/schemas/stratz_query_leagues.output.json), [examples](../internal/contracts/generated/examples/stratz_query_leagues.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_query_leagues.json).

### `stratz_query_matches`

Query normalized matches by exact IDs, history filters, or live filters.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_query_matches.input.json), [output schema](../internal/contracts/generated/schemas/stratz_query_matches.output.json), [examples](../internal/contracts/generated/examples/stratz_query_matches.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_query_matches.json).

### `stratz_query_players`

Query normalized players by one to 25 exact identifiers.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_query_players.input.json), [output schema](../internal/contracts/generated/schemas/stratz_query_players.output.json), [examples](../internal/contracts/generated/examples/stratz_query_players.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_query_players.json).

### `stratz_server_info`

Return server, protocol, schema, cache, limits, and upstream status.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_server_info.input.json), [output schema](../internal/contracts/generated/schemas/stratz_server_info.output.json), [examples](../internal/contracts/generated/examples/stratz_server_info.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_server_info.json).

All outputs use the generated success/error envelope. The linked output schemas are authoritative for payload shapes, bounds, and tool-specific error details.
