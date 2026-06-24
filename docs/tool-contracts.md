---
Created: 2026-06-18
Purpose: Define normative MCP wire behavior, detail-level inclusion rules, and examples for the STRATZ MCP tools.
Status: Normative planning contract; implementation mapping remains a milestone deliverable
---

# STRATZ MCP tool contracts

## 1. Normative sources

The machine-readable source of truth is [tool-contracts.json](./tool-contracts.json).

It contains every v1 tool's:

- Description.
- Draft 2020-12 `inputSchema`.
- Draft 2020-12 `outputSchema`.
- Shared normalized record definitions.
- Stable error-code enum.

Build tooling must dereference the registry's shared `$defs` before publishing each MCP `Tool.inputSchema` and `Tool.outputSchema`. Generated Go types, validators, protocol fixtures, and reference documentation must originate from this registry. CI fails when generated artifacts differ.

The schemas are normative for the project's public MCP contract. Remaining field-feasibility and upstream-mapping work is an implementation deliverable; if STRATZ cannot supply a required field, change this contract explicitly rather than silently weakening it.

## 2. MCP wire contract

- Protocol target: MCP `2025-11-25`.
- Transport: stdio only in v1.
- Schema dialect: JSON Schema Draft 2020-12.
- Every tool publishes both `inputSchema` and `outputSchema`.
- Every successful or execution-error call returns one `content` text item containing the exact compact JSON serialization of `structuredContent`.
- `structuredContent` is authoritative.
- The text mirror exists for clients that do not expose structured content.
- The text mirror must not contain Markdown fences or commentary.
- Tool results must conform to the tool's declared `outputSchema`.
- A normal result uses `{"kind":"success", ...}` and MCP `isError` is absent or `false`.
- An execution error uses `{"kind":"error","error":{...}}` and MCP `isError: true`.
- Invalid JSON-RPC, unknown tools, and malformed `tools/call` envelopes are protocol errors.
- Schema-valid MCP calls with invalid values, upstream failures, cache failures, and business errors are tool execution errors.
- The server declares `tools`, `resources`, and `prompts` capabilities. `listChanged` is `false` because the v1 surface is static for a running process.
- Resources do not advertise subscriptions in v1.
- The server negotiates protocol versions through MCP initialization. It prefers `2025-11-25` and fails initialization clearly when the client and Go SDK cannot negotiate a supported version.

Raw GraphQL uses the machine-readable `rawGraphqlPolicy` in [tool-contracts.json](./tool-contracts.json):

- Query operation type is necessary but not sufficient.
- The default root-field action is deny.
- Approved roots are `constants`, `guild`, `heroStats`, `leaderboard`, `league`, `leagues`, `live`, `match`, `matches`, `player`, `players`, `team`, and `teams`.
- `plus`, `stratz`, `vendor`, `yogurt`, unknown future roots, mutations, and subscriptions are denied.
- `__typename` is allowed; `__schema` and `__type` require the runtime introspection flag.
- Root checks run after fragment expansion and against underlying field names rather than aliases.

Example success:

```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"kind\":\"success\",\"data\":{\"server_version\":\"1.0.0\",\"mcp_protocol_version\":\"2025-11-25\",\"schema_version\":\"sha256:...\",\"cache_status\":\"healthy\",\"upstream_status\":\"reachable\",\"limits\":{}},\"summary\":null,\"provenance\":{\"retrieved_at\":\"2026-06-18T13:00:00Z\",\"operation\":\"server_info\",\"schema_version\":\"sha256:...\",\"detail_level\":null,\"cache\":{\"status\":\"disabled\",\"age_seconds\":null},\"patch\":null,\"date_range\":null,\"rate_limits\":[]},\"warnings\":[]}"
    }
  ],
  "structuredContent": {
    "kind": "success",
    "data": {
      "server_version": "1.0.0",
      "mcp_protocol_version": "2025-11-25",
      "schema_version": "sha256:...",
      "cache_status": "healthy",
      "upstream_status": "reachable",
      "limits": {}
    },
    "summary": null,
    "provenance": {
      "retrieved_at": "2026-06-18T13:00:00Z",
      "operation": "server_info",
      "schema_version": "sha256:...",
      "detail_level": null,
      "cache": {
        "status": "disabled",
        "age_seconds": null
      },
      "patch": null,
      "date_range": null,
      "rate_limits": []
    },
    "warnings": []
  }
}
```

Example execution error:

```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"kind\":\"error\",\"error\":{\"code\":\"INVALID_ARGUMENT\",\"message\":\"match_id must contain decimal digits only\",\"retryable\":false,\"retry_after\":null,\"details\":{\"field\":\"match_id\"}}}"
    }
  ],
  "structuredContent": {
    "kind": "error",
    "error": {
      "code": "INVALID_ARGUMENT",
      "message": "match_id must contain decimal digits only",
      "retryable": false,
      "retry_after": null,
      "details": {
        "field": "match_id"
      }
    }
  },
  "isError": true
}
```

## 3. Common result rules

- `raw` is omitted unless the caller requested `include_raw: true`.
- `raw` is never included in an execution error.
- In v1, only `DATA_NOT_READY` includes `context`; all other execution errors must omit it.
- `summary` is at most 4 KiB and contains no new facts beyond `data`.
- `warnings` are non-fatal and must not contain instructions to the agent.
- `provenance.operation` is the stable internal curated-operation name, not the raw GraphQL document.
- Unknown upstream fields are discarded from normalized data.
- User-controlled strings are Unicode-normalized, stripped of prohibited control characters, and truncated to schema limits.
- URLs are data only. Neither tools nor skills follow URLs returned by STRATZ.

## 4. Detail-level inclusion

Fields listed for a lower level are present at every higher level unless the schema marks them nullable. Fields not listed for the selected level are omitted.

### 4.1 Player

| Level | Required fields |
|---|---|
| `summary` | `account_id`, `steam_id64`, `display_name`, `avatar_url`, `is_private`, `rank` |
| `standard` | Summary plus `match_count`, `win_count`, `last_match_at` |
| `full` | Same normalized v1 fields as standard; `include_raw` is required for additional upstream fields |

The server never adds cross-service enrichment. A private profile is an execution error and is not cached.

### 4.2 Match

| Level | Required fields |
|---|---|
| `summary` | Match summary fields plus ten-or-fewer `players` |
| `standard` | Summary plus bounded `objectives`; omit `timeline`, `fights`, and `economy` |
| `full` | Standard plus bounded `timeline`; omit unavailable `fights` and `economy` with an explicit warning |

If replay-derived data required by the requested level is unavailable, return `DATA_NOT_READY`; do not silently downgrade. This is an error-only result with MCP `isError: true`. Its required `context` is:

```json
{
  "kind": "error",
  "error": {
    "code": "DATA_NOT_READY",
    "message": "Full match detail is not parsed yet",
    "retryable": true,
    "retry_after": null,
    "details": {}
  },
  "context": {
    "type": "match_availability",
    "match": {
      "match_id": "8000000000",
      "started_at": "2026-06-17T19:30:00Z",
      "duration_seconds": 2430,
      "radiant_win": true,
      "radiant_score": 42,
      "dire_score": 31,
      "game_mode_id": 22,
      "lobby_type_id": 7,
      "region_id": 3,
      "league_id": null,
      "patch_id": "7.XX",
      "parse_status": "pending"
    },
    "requested_detail_level": "full"
  }
}
```

This object is returned as authoritative `structuredContent`, mirrored exactly in the text content item, and accompanied by MCP `isError: true`. No success `data`, `summary`, `provenance`, or `raw` fields accompany it.

### 4.3 Hero

All levels return the complete normalized `hero` record. Detail level is retained for consistency and future-compatible expansion.

### 4.4 League

All levels return the complete normalized `league` record. Team rosters and player details are not part of v1's curated league record.

### 4.5 Lists and batches

- Player-match and league-match list items are `matchSummary` records at every level.
- `stratz_list_live_matches` always returns complete normalized `liveMatch` records.
- Batch item fields follow the corresponding singular tool's detail rules.
- Successful batch item order and duplicates exactly match the input.

## 5. Tool examples

Examples omit most provenance fields only for readability; actual responses must satisfy the full schema.

### `stratz_server_info`

Input:

```json
{}
```

Data:

```json
{
  "server_version": "1.0.0",
  "mcp_protocol_version": "2025-11-25",
  "schema_version": "sha256:...",
  "cache_status": "healthy",
  "upstream_status": "reachable",
  "limits": {
    "response_bytes": 5242880,
    "raw_complexity": 1000
  }
}
```

### `stratz_get_player`

Input:

```json
{
  "player_id": "76561198000000000",
  "detail_level": "standard"
}
```

Data:

```json
{
  "account_id": "39734272",
  "steam_id64": "76561198000000000",
  "display_name": "Example",
  "avatar_url": null,
  "is_private": false,
  "rank": {
    "rank_tier": 65,
    "leaderboard_rank": null
  },
  "match_count": 1200,
  "win_count": 620,
  "last_match_at": "2026-06-17T19:30:00Z"
}
```

### `stratz_list_player_matches`

Input:

```json
{
  "player_id": "39734272",
  "limit": 20,
  "result": "win"
}
```

Data:

```json
{
  "items": [
    {
      "match_id": "8000000000",
      "started_at": "2026-06-17T19:30:00Z",
      "duration_seconds": 2430,
      "radiant_win": true,
      "radiant_score": 42,
      "dire_score": 31,
      "game_mode_id": 22,
      "lobby_type_id": 7,
      "region_id": 3,
      "league_id": null,
      "patch_id": "7.XX",
      "parse_status": "parsed"
    }
  ],
  "page": {
    "next_cursor": "v1....",
    "has_more": true
  }
}
```

### `stratz_batch_get_players`

Input:

```json
{
  "player_ids": ["39734272", "76561198000000000"],
  "detail_level": "summary"
}
```

Data:

```json
{
  "items": [
    {
      "account_id": "39734272",
      "steam_id64": "76561198000000000",
      "display_name": "Example",
      "avatar_url": null,
      "is_private": false,
      "rank": null
    },
    {
      "account_id": "39734272",
      "steam_id64": "76561198000000000",
      "display_name": "Example",
      "avatar_url": null,
      "is_private": false,
      "rank": null
    }
  ]
}
```

### `stratz_get_match`

Input:

```json
{
  "match_id": "8000000000",
  "detail_level": "standard"
}
```

Data:

```json
{
  "match_id": "8000000000",
  "started_at": "2026-06-17T19:30:00Z",
  "duration_seconds": 2430,
  "radiant_win": true,
  "radiant_score": 42,
  "dire_score": 31,
  "game_mode_id": 22,
  "lobby_type_id": 7,
  "region_id": 3,
  "league_id": null,
  "patch_id": "7.XX",
  "parse_status": "parsed",
  "players": [],
  "objectives": []
}
```

### `stratz_batch_get_matches`

Input:

```json
{
  "match_ids": ["8000000000"],
  "detail_level": "summary"
}
```

Data:

```json
{
  "items": [
    {
      "match_id": "8000000000",
      "started_at": null,
      "duration_seconds": null,
      "radiant_win": null,
      "radiant_score": null,
      "dire_score": null,
      "game_mode_id": null,
      "lobby_type_id": null,
      "region_id": null,
      "league_id": null,
      "patch_id": null,
      "parse_status": "unknown",
      "players": []
    }
  ]
}
```

### `stratz_get_hero`

Input:

```json
{
  "hero": "crystal-maiden"
}
```

Data:

```json
{
  "hero_id": 5,
  "name": "npc_dota_hero_crystal_maiden",
  "slug": "crystal-maiden",
  "localized_name": "Crystal Maiden",
  "primary_attribute": "intelligence",
  "attack_type": "ranged",
  "roles": ["support", "nuker", "disabler"]
}
```

### `stratz_get_hero_stats`

Input:

```json
{
  "hero": 5,
  "patch_id": "7.XX",
  "include_matchups": true
}
```

Data:

```json
{
  "hero_id": 5,
  "sample_size": 10000,
  "pick_rate": 0.12,
  "win_rate": 0.51,
  "ban_rate": 0.03,
  "roles": [],
  "lanes": [],
  "matchups": [],
  "synergies": []
}
```

### `stratz_batch_get_heroes`

Input:

```json
{
  "heroes": [5]
}
```

Data:

```json
{
  "items": [
    {
      "hero_id": 5,
      "name": "npc_dota_hero_crystal_maiden",
      "slug": "crystal-maiden",
      "localized_name": "Crystal Maiden",
      "primary_attribute": "intelligence",
      "attack_type": "ranged",
      "roles": ["support"]
    }
  ]
}
```

### `stratz_list_leagues`

Input:

```json
{
  "query": "International",
  "limit": 20
}
```

Data:

```json
{
  "items": [
    {
      "league_id": "12345",
      "name": "Example League",
      "tier": "premium",
      "status": "completed",
      "region": "international",
      "starts_at": "2026-01-01T00:00:00Z",
      "ends_at": "2026-01-15T00:00:00Z"
    }
  ],
  "page": {
    "next_cursor": null,
    "has_more": false
  }
}
```

### `stratz_get_league`

Input:

```json
{
  "league_id": "12345"
}
```

Data is one `league` record with the same shape shown above.

### `stratz_list_league_matches`

Input:

```json
{
  "league_id": "12345",
  "limit": 20
}
```

Data contains `items` of `matchSummary` plus `page`.

### `stratz_list_live_matches`

Input:

```json
{
  "minimum_spectators": 1000,
  "sort": "highest_profile",
  "limit": 20
}
```

Data:

```json
{
  "items": [
    {
      "match_id": "8000000002",
      "started_at": "2026-06-18T12:30:00Z",
      "duration_seconds": 1800,
      "game_mode_id": 22,
      "spectator_count": 15000,
      "league": null,
      "radiant_team_name": "Radiant",
      "dire_team_name": "Dire",
      "players": []
    }
  ],
  "page": {
    "next_cursor": null,
    "has_more": false
  }
}
```

### `stratz_get_constants`

Input:

```json
{
  "type": "heroes"
}
```

Data:

```json
{
  "type": "heroes",
  "items": [
    {
      "id": "5",
      "name": "npc_dota_hero_crystal_maiden",
      "localized_name": "Crystal Maiden",
      "metadata": {
        "slug": "crystal-maiden"
      }
    }
  ]
}
```

### `stratz_execute_graphql`

Input:

```json
{
  "query": "query Match($id: Long!) { match(id: $id) { id } }",
  "variables": {
    "id": "8000000000"
  },
  "operation_name": "Match"
}
```

Data:

```json
{
  "graphql": {
    "data": {
      "match": {
        "id": "8000000000"
      }
    },
    "errors": [],
    "extensions": null
  },
  "partial": false,
  "http_status": 200
}
```

## 6. Contract review rule

Any change to a tool name, schema, detail-level inclusion rule, error code, cursor behavior, or MCP wire mapping requires:

1. A contract-version change.
2. Updated generated validators and examples.
3. Compatibility tests against Codex and Claude.
4. A migration or deprecation note when an existing client could break.

### `1.0.0-draft.3` migration note

Live-match players now use a dedicated shape where `hero_id` and `won` are nullable. Clients generated from `1.0.0-draft.2` must accept `null` while a hero is unselected and while the match outcome is not yet known. Completed-match player records are unchanged.
