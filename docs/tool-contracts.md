---
Created: 2026-06-18
Updated: 2026-10-04
Purpose: Define the normative v2 MCP wire contract and query semantics for STRATZ MCP.
Status: Normative; the machine-readable registry is the source of truth.
---

# STRATZ MCP tool contracts

## 1. Contract boundary

[`tool-contracts.json`](./tool-contracts.json) is the canonical Draft 2020-12 registry. It defines exactly seven tools:

- `stratz_server_info`
- `stratz_query_players`
- `stratz_query_matches`
- `stratz_query_heroes`
- `stratz_query_leagues`
- `stratz_query_constants`
- `stratz_execute_graphql`

The registry version is `2.0.0-draft.1`. This is an intentional breaking boundary from the thirteen curated v1 retrieval tools. There are no compatibility aliases, and a client must not infer a mode from optional fields.

Every query tool input is a closed `oneOf` whose branches require a `mode` constant. A request containing fields from another branch fails validation. Every successful query response has the common envelope and `data.mode` plus `data.items`; only paginated modes include `data.page`. `warnings` is non-fatal, and `provenance` records retrieval, operation, schema, cache, date, patch, and rate-limit facts. `raw` is present only when explicitly requested and is never returned with an error.

The server uses the MCP `2026-07-28` preferred protocol and continues to advertise the existing supported-version set. The first text content item is the compact JSON serialization of authoritative `structuredContent`. Tool execution failures use `kind: "error"` and MCP `isError: true`; malformed JSON-RPC remains a protocol error.

## 2. Shared query rules

- Exact selectors accept 1–25 identifiers, normalize identifiers before lookup, preserve input order and duplicates, and are atomic: one not-found, private, invalid, or upstream failure fails the complete call without a partial `items` result.
- Exact selectors do not accept pagination or unrelated filters. Search and history selectors do not accept exact identifier arrays.
- A query consumes no more than five upstream attempts. Client-side filtering is bounded and reports incomplete scans in `warnings` with a continuation cursor where the mode is paginated.
- `fresh` bypasses cache reads and stale fallback; `include_raw` requests a bounded upstream payload and bypasses curated cache persistence.
- Opaque cursors are authenticated, filter-bound, versioned, and expiry-bound. Cursors from the v1 contract, another mode, another filter, or another token namespace are invalid. Existing cursors are intentionally invalidated at this boundary.
- Cache entries are cold-started at this boundary; v1 entries are not reused. Cache status and stale behavior are exposed through `provenance`.
- Normalized strings are Unicode-normalized, control characters are removed, and schema limits are applied. Returned URLs are inert data.
- Private profiles return `PRIVATE`; missing entities return `NOT_FOUND`; parsed-data gaps return `DATA_NOT_READY` where the mode requires that data. Exact failures are atomic.

## 3. Query tools

### 3.1 `stratz_query_players`

The only mode is `exact`. `player_ids` contains Steam account IDs, SteamID64 values, or STRATZ profile URLs and is normalized to canonical account identifiers. The result is `data.mode: "exact"` with player items in the original order, including duplicates. `include_profile_statistics` is default-off; statistics are returned only when requested. General player search is not supported by this contract.

Example input:

```json
{"mode":"exact","player_ids":["39734272","76561198000000000"],"include_profile_statistics":true}
```

### 3.2 `stratz_query_matches`

The modes are:

- `exact`: `match_ids` contains 1–25 decimal IDs and may include duplicates; items are complete normalized matches at the requested bounded detail level.
- `player_history`: `player_id` is required; date, hero, role, game-mode, lobby, result, minimum-duration, and patch filters are supported. Minimum duration is bounded local filtering. Items are tagged player-match summaries: omitted detail defaults to `summary`; `detail_level: "players"` includes the optional `player` property as a player object or `null`. `standard` and `full` are rejected because history returns no such shape. The result is paginated.
- `league_history`: `league_id` is required; date and patch filters are supported. Omitted detail and the only accepted `detail_level` are `summary`; `standard`, `players`, and `full` are rejected because items are closed match summaries. The result is paginated.
- `live`: optional player, team, league, hero, game-state, tier, game-mode, and minimum-spectator filters are bounded by the five-attempt budget. `sort` is `newest` or `highest_profile`; live mode has no detail level; items are live matches and the result is paginated.

Exact mode accepts `summary`, `players`, `standard`, and `full`, defaulting to `standard`; these levels select only fields that the committed upstream mapping can provide. `DATA_NOT_READY` is returned instead of silently downgrading replay-dependent detail.

### 3.3 `stratz_query_heroes`

The modes are:

- `exact`: `heroes` contains 1–25 numeric IDs, exact localized names, or canonical slugs and preserves order and duplicates.
- `search`: `query` performs bounded local/indexed matching and is paginated.

Both modes support optional date, rank-bracket, and role constraints for statistics. `include_statistics` defaults to false. When enabled, each hero item may carry typed `statistics` containing sample size, pick rate, and win rate for the effective population. No lane, item, build, matchup, synergy, or other unsupported hero dimension is promised.

### 3.4 `stratz_query_leagues`

The modes are:

- `exact`: `league_ids` contains 1–25 exact IDs and preserves order and duplicates.
- `search`: optional name query, status, tier, and date filters use authenticated bounded pagination. Items are leagues and `page` is always present.

Status is normalized from the upstream live, future, ended, and date values. Search may be incomplete after the request budget and must say so in `warnings` rather than claiming exhaustive results.

### 3.5 `stratz_query_constants`

The modes are:

- `types`: `types` is a non-empty array of one or more supported classes: `heroes`, `items`, `abilities`, `game_modes`, `regions`, or `game_versions`.
- `typed_selectors`: `selectors` contains typed class/ID arrays for bounded exact retrieval.

There is no `all` shortcut, no rank class, and no untyped selector. A `types` array expresses multi-class retrieval explicitly. Constant items carry their class in `type`; unsupported classes are rejected rather than fabricated.

## 4. Raw GraphQL

`stratz_execute_graphql` remains guarded and is not a bypass for curated validation. It accepts exactly one query operation, applies the default-deny root policy in the JSON registry, rejects mutations, subscriptions, denied/unknown roots, unapproved introspection, oversized documents, unbounded lists, excessive depth/complexity, and unsafe response sizes. It allows only JSON-compatible variables. A response containing both GraphQL data and errors is marked partial and retains both values. Raw caching remains opt-in, classified, and never serves stale results.

## 5. Wire and privacy guarantees

Curated results use:

```json
{
  "kind":"success",
  "data":{"mode":"...","items":[]},
  "summary":null,
  "provenance":{},
  "warnings":[]
}
```

Paginated data adds `page: {"next_cursor":null,"has_more":false}`. Errors contain only the stable error envelope and safe details. Credentials, authorization headers, private upstream headers, and token fingerprints never appear in `raw`, `provenance`, or errors. Static resources are unchanged by this contract boundary.

## 6. v1 to v2 migration

| v1 surface | v2 replacement and translation |
|---|---|
| `stratz_get_player` | `stratz_query_players` with `{"mode":"exact","player_ids":[player_id]}`; profile statistics are opt-in. |
| `stratz_batch_get_players` | `stratz_query_players` with `player_ids`; order, duplicates, and all-or-nothing failure are preserved. |
| `stratz_list_player_matches` | `stratz_query_matches` with `mode: "player_history"`; move `player_id` and existing filters unchanged where supported. |
| `stratz_get_match` | `stratz_query_matches` with `mode: "exact","match_ids":[match_id]`; detail remains bounded. |
| `stratz_batch_get_matches` | `stratz_query_matches` with `mode: "exact","match_ids`; the response is `data.mode` plus `items`. |
| `stratz_get_hero` | `stratz_query_heroes` with `mode: "exact","heroes":[hero]`; statistics are no longer implicit. |
| `stratz_get_hero_stats` | `stratz_query_heroes` with exact or search mode and `include_statistics:true`; unsupported dimensions are removed. |
| `stratz_batch_get_heroes` | `stratz_query_heroes` with `mode: "exact","heroes`; preserve order and duplicates. |
| `stratz_list_leagues` | `stratz_query_leagues` with `mode: "search"` and the supported filters. |
| `stratz_get_league` | `stratz_query_leagues` with `mode: "exact","league_ids":[league_id]`. |
| `stratz_list_league_matches` | `stratz_query_matches` with `mode: "league_history","league_id"`. |
| `stratz_list_live_matches` | `stratz_query_matches` with `mode: "live"` and supported live filters. |
| `stratz_get_constants` | `stratz_query_constants` with `mode: "types","types":[...]` or `mode: "typed_selectors"`; `all` is removed. |
| All old names and aliases | Removed; no old tool alias remains valid. |
| v1 response payloads | Read `data.mode` and `data.items`; paginated results read `data.page`; item shapes remain mode-specific. |
| v1 cursors | Invalidated; clients must start a new traversal and handle `CURSOR_INVALID` or `CURSOR_EXPIRED`. |
| v1 cache entries | Not read by v2; caches cold-start under the v2 contract. |

Clients must discover the seven-tool catalog again and must not send old arguments to a new mode branch. This table is migration guidance; the JSON registry controls validation.

## 7. Change rule

Any tool-name, mode, field, output-shape, error, cursor, cache, or MCP wire change requires a contract-version decision, registry update, generated-artifact update in the subsequent generation task, compatibility tests, and a migration note when existing clients can break.
