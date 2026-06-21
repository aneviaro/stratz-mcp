---
Created: 2026-06-19
Purpose: Generated reference for the public STRATZ MCP tool contracts.
Status: Generated from docs/tool-contracts.json; do not edit manually
---

# Generated STRATZ MCP tool contracts

- Contract version: `1.0.0-draft.3`
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

## Tool details

### `stratz_batch_get_heroes`

Get up to 25 heroes atomically; any item failure or ambiguity fails the entire batch.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `heroes` | true | `array`; minItems `1`; maxItems `25` |
| `include_raw` | false | `boolean`; default `false` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_batch_get_heroes.input.json), [output schema](../internal/contracts/generated/schemas/stratz_batch_get_heroes.output.json), [examples](../internal/contracts/generated/examples/stratz_batch_get_heroes.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_batch_get_heroes.json).

### `stratz_batch_get_matches`

Get up to 25 matches atomically; any item failure fails the entire batch.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `match_ids` | true | `array`; minItems `1`; maxItems `25` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_batch_get_matches.input.json), [output schema](../internal/contracts/generated/schemas/stratz_batch_get_matches.output.json), [examples](../internal/contracts/generated/examples/stratz_batch_get_matches.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_batch_get_matches.json).

### `stratz_batch_get_players`

Get up to 25 players atomically; any item failure fails the entire batch.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `player_ids` | true | `array`; minItems `1`; maxItems `25` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_batch_get_players.input.json), [output schema](../internal/contracts/generated/schemas/stratz_batch_get_players.output.json), [examples](../internal/contracts/generated/examples/stratz_batch_get_players.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_batch_get_players.json).

### `stratz_execute_graphql`

Execute one guarded GraphQL query against an approved STRATZ root field using JSON-compatible variables.

| Input | Required | Type and constraints |
|---|---:|---|
| `cache` | false | `boolean`; default `false` |
| `cache_ttl_seconds` | false | `integer`; minimum `1`; maximum `3600` |
| `fresh` | false | `boolean`; default `false` |
| `operation_name` | false | `string`; minLength `1`; maxLength `256` |
| `query` | true | `string`; minLength `1`; maxLength `65536` |
| `variables` | false | `object` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_execute_graphql.input.json), [output schema](../internal/contracts/generated/schemas/stratz_execute_graphql.output.json), [examples](../internal/contracts/generated/examples/stratz_execute_graphql.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_execute_graphql.json).

### `stratz_get_constants`

Get one explicit class of STRATZ/Dota reference constants, or all classes when requested.

| Input | Required | Type and constraints |
|---|---:|---|
| `fresh` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `type` | true | `string`; one of `heroes`, `items`, `abilities`, `game_modes`, `regions`, `ranks`, `all` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_get_constants.input.json), [output schema](../internal/contracts/generated/schemas/stratz_get_constants.output.json), [examples](../internal/contracts/generated/examples/stratz_get_constants.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_get_constants.json).

### `stratz_get_hero`

Get normalized hero reference data by numeric ID, exact localized name, or canonical slug.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `hero` | true | `heroIdentifier` |
| `include_raw` | false | `boolean`; default `false` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_get_hero.input.json), [output schema](../internal/contracts/generated/schemas/stratz_get_hero.output.json), [examples](../internal/contracts/generated/examples/stratz_get_hero.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_get_hero.json).

### `stratz_get_hero_stats`

Get bounded aggregate hero statistics for a date, patch, rank, role, and lane window.

| Input | Required | Type and constraints |
|---|---:|---|
| `fresh` | false | `boolean`; default `false` |
| `from` | false | `dateTime` |
| `hero` | true | `heroIdentifier` |
| `include_matchups` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `include_synergies` | false | `boolean`; default `false` |
| `lane` | false | `string`; maxLength `64` |
| `patch_id` | false | `string`; maxLength `64` |
| `rank_bracket` | false | `string`; maxLength `64` |
| `role` | false | `string`; maxLength `64` |
| `to` | false | `dateTime` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_get_hero_stats.input.json), [output schema](../internal/contracts/generated/schemas/stratz_get_hero_stats.output.json), [examples](../internal/contracts/generated/examples/stratz_get_hero_stats.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_get_hero_stats.json).

### `stratz_get_league`

Get normalized league metadata by exact league ID.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `league_id` | true | `string`; minLength `1`; maxLength `32` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_get_league.input.json), [output schema](../internal/contracts/generated/schemas/stratz_get_league.output.json), [examples](../internal/contracts/generated/examples/stratz_get_league.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_get_league.json).

### `stratz_get_match`

Get a normalized Dota match with detail-level controlled timelines and replay-derived events.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `match_id` | true | `matchId` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_get_match.input.json), [output schema](../internal/contracts/generated/schemas/stratz_get_match.output.json), [examples](../internal/contracts/generated/examples/stratz_get_match.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_get_match.json).

### `stratz_get_player`

Get a normalized STRATZ player profile by account ID, SteamID64, or STRATZ profile URL.

| Input | Required | Type and constraints |
|---|---:|---|
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `include_raw` | false | `boolean`; default `false` |
| `player_id` | true | `playerIdentifier` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_get_player.input.json), [output schema](../internal/contracts/generated/schemas/stratz_get_player.output.json), [examples](../internal/contracts/generated/examples/stratz_get_player.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_get_player.json).

### `stratz_list_league_matches`

List matches for a league with bounded date/patch filters and an authenticated opaque cursor.

| Input | Required | Type and constraints |
|---|---:|---|
| `cursor` | false | `string`; maxLength `4096` |
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `from` | false | `dateTime` |
| `include_raw` | false | `boolean`; default `false` |
| `league_id` | true | `string`; minLength `1`; maxLength `32` |
| `limit` | false | `integer`; default `20`; minimum `1`; maximum `100` |
| `patch_id` | false | `string`; maxLength `64` |
| `to` | false | `dateTime` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_list_league_matches.input.json), [output schema](../internal/contracts/generated/schemas/stratz_list_league_matches.output.json), [examples](../internal/contracts/generated/examples/stratz_list_league_matches.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_list_league_matches.json).

### `stratz_list_leagues`

Search and list STRATZ leagues with bounded filters and an authenticated opaque cursor.

| Input | Required | Type and constraints |
|---|---:|---|
| `cursor` | false | `string`; maxLength `4096` |
| `fresh` | false | `boolean`; default `false` |
| `from` | false | `dateTime` |
| `include_raw` | false | `boolean`; default `false` |
| `limit` | false | `integer`; default `20`; minimum `1`; maximum `100` |
| `query` | false | `string`; maxLength `256` |
| `status` | false | `string`; one of `live`, `ongoing`, `completed`, `ended`, `upcoming`, `future` |
| `tier` | false | `string`; maxLength `64` |
| `to` | false | `dateTime` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_list_leagues.input.json), [output schema](../internal/contracts/generated/schemas/stratz_list_leagues.output.json), [examples](../internal/contracts/generated/examples/stratz_list_leagues.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_list_leagues.json).

### `stratz_list_live_matches`

List current live matches with bounded filters, sorting, and a short-lived authenticated cursor.

| Input | Required | Type and constraints |
|---|---:|---|
| `cursor` | false | `string`; maxLength `4096` |
| `fresh` | false | `boolean`; default `false` |
| `game_mode_id` | false | `integer` |
| `game_states` | false | `array`; maxItems `16` |
| `hero` | false | `heroIdentifier` |
| `include_raw` | false | `boolean`; default `false` |
| `league_id` | false | `string`; maxLength `32` |
| `limit` | false | `integer`; default `20`; minimum `1`; maximum `100` |
| `minimum_spectators` | false | `integer`; minimum `0` |
| `player_id` | false | `playerIdentifier` |
| `sort` | false | `string`; one of `newest`, `highest_profile`; default `highest_profile` |
| `team_id` | false | `string`; maxLength `32` |
| `tiers` | false | `array`; maxItems `10` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_list_live_matches.input.json), [output schema](../internal/contracts/generated/schemas/stratz_list_live_matches.output.json), [examples](../internal/contracts/generated/examples/stratz_list_live_matches.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_list_live_matches.json).

### `stratz_list_player_matches`

List normalized matches for a player using bounded filters and an authenticated opaque cursor.

| Input | Required | Type and constraints |
|---|---:|---|
| `cursor` | false | `string`; maxLength `4096` |
| `detail_level` | false | `detailLevel` |
| `fresh` | false | `boolean`; default `false` |
| `from` | false | `dateTime` |
| `game_mode_id` | false | `integer` |
| `hero` | false | `heroIdentifier` |
| `include_raw` | false | `boolean`; default `false` |
| `limit` | false | `integer`; default `20`; minimum `1`; maximum `100` |
| `lobby_type_id` | false | `integer` |
| `minimum_duration_seconds` | false | `integer`; minimum `0`; maximum `21600` |
| `patch_id` | false | `string`; maxLength `64` |
| `player_id` | true | `playerIdentifier` |
| `result` | false | `string`; one of `win`, `loss` |
| `role` | false | `string`; maxLength `64` |
| `to` | false | `dateTime` |

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_list_player_matches.input.json), [output schema](../internal/contracts/generated/schemas/stratz_list_player_matches.output.json), [examples](../internal/contracts/generated/examples/stratz_list_player_matches.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_list_player_matches.json).

### `stratz_server_info`

Return server, protocol, schema, cache, limit, and upstream connectivity information without secrets.

Inputs: none.

Artifacts: [input schema](../internal/contracts/generated/schemas/stratz_server_info.input.json), [output schema](../internal/contracts/generated/schemas/stratz_server_info.output.json), [examples](../internal/contracts/generated/examples/stratz_server_info.input.json), and [JSON-RPC fixture](../internal/contracts/generated/protocol/stratz_server_info.json).

All outputs use the generated success/error envelope. The linked output schemas are authoritative for payload shapes, bounds, and tool-specific error details.
