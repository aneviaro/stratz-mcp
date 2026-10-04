---
Created: 2026-06-18
Updated: 2026-10-04
Purpose: Map the v2 query modes to verified STRATZ operations and bounded local behavior.
Status: v2 contract feasibility recorded; exact operation generation remains a later implementation task.
---

# STRATZ schema feasibility for v2 query modes

## 1. Evidence boundary

Authenticated introspection was performed against `https://api.stratz.com/graphql` using the repository's verified HTTP contract. The fetched schema is not committed or redistributed. This document records only native fields and bounded derivations supported by the evidence; it does not authorize unsupported fields.

The Dota query root exposes `constants`, `heroStats`, `league`, `leagues`, `live`, `match`, `matches`, `player`, `players`, `team`, and `teams`. Raw access remains governed by the default-deny policy in [`tool-contracts.json`](./tool-contracts.json).

All curated calls remain within five upstream attempts. Local scans must be bounded, preserve continuation state in an authenticated cursor, and warn when the result is incomplete.

## 2. `stratz_query_players`

### Exact mode

- Native source: `players(steamAccountIds: [Long]!)`, with `player(steamAccountId: Long!)` for a single normalized identifier.
- Feasible identifiers: Steam account ID, SteamID64, and STRATZ profile URL after local normalization.
- Native normalized fields: account identity, display identity, avatar, privacy state, rank/leaderboard values, match count, win count, and last-match time where returned.
- `include_profile_statistics` is default-off; when enabled it maps only the verified count/win/last-match fields.
- The 25-item bound is feasible in one bounded batch operation. Upstream null/private results are mapped to the documented atomic error behavior.

No general player search mode is defined because the verified schema does not provide a safe bounded search contract.

## 3. `stratz_query_matches`

### Exact mode

- Native source: `matches(ids: [Long]!)`; one request supports up to 25 IDs.
- Match IDs are decimal strings, and native result order is normalized back to exact input order with duplicates.
- Summary, player rows, objectives, and bounded timeline fields are available from `MatchType` and `MatchPlaybackDataType`.
- A missing match is `NOT_FOUND`; an unavailable replay-dependent detail is `DATA_NOT_READY`, not a silent downgrade.

### Player-history mode

- Native source: `player.matches(request: PlayerMatchesRequestType!)`.
- Native filters include date bounds, hero, position/role/lane IDs, game mode, lobby, victory, patch/game version, league, region, parsed/stats, party, radiant, and team/player relations.
- The public v2 subset is date, hero, role, game mode, lobby, result, patch, and minimum duration. Minimum duration is a bounded local filter because it is not native.
- `take`, `skip`, `before`, and `after` are carried in the authenticated cursor; no cursor exposes upstream credentials or private data.

### League-history mode

- Native source: `league.matches(request: LeagueMatchesRequestType!)`.
- Date, game mode/version, hero, parsed/stats, lane/role/position, lobby, rank, region, series, team/player, and pagination inputs are available upstream.
- The v2 public subset is league ID, date, and patch; results are normalized to match summaries and are paginated.

### Live mode

- Native source: `live.matches(request: MatchLiveRequestType)`.
- Native filters include game state, hero, completion/parsing state, league, league tier, ordering, and pagination.
- Player, team, game mode, and minimum spectator constraints are bounded local filters over live pages.
- Supported public order values are `newest` and `highest_profile`, mapped to the verified order fields.
- Live results provide match, game mode/state, league/team, players, spectator count, scores, and rank-related live fields. Region filtering and replay-derived fields are not promised.

## 4. `stratz_query_heroes`

### Exact and search modes

- Native source: `constants.hero(id:)` and the bounded `constants.heroes(...)` aggregate; exact name/slug matching uses a local normalized index and rejects ambiguity.
- Exact mode accepts up to 25 IDs, names, or slugs and performs local selection after the bounded constants load.
- Search mode is a bounded local/indexed search over the same hero reference set and is paginated when needed.
- Normalized reference fields are hero ID, internal name, slug/localized name, primary attribute, attack type, and roles.

### Optional statistics

- Native source: `heroStats.stats` plus the verified time-bucketed win operations.
- Supported public constraints are date range, rank bracket, and role. Date ranges are translated into bounded day/week/month buckets; provenance records the effective range and warnings record rounding.
- Sample size, pick rate, and win rate are feasible from the verified aggregate population. Metrics without the requested dimension return `INVALID_ARGUMENT` or an explicitly unavailable field; populations are never mixed.
- Lane dimensions, item/build/talent guides, matchups, synergies, and other statistics are not part of the v2 contract even where related upstream fields exist.
- Statistics are not fetched unless `include_statistics: true`.

## 5. `stratz_query_leagues`

### Exact mode

- Native source: `leagues(request: LeagueRequestType!)` with the requested ID set, with bounded deduplication and atomic response semantics in one paged operation.
- League ID, name, region, tier, date range, and live/status inputs are available for normalized league records.

### Search mode

- Native source: `leagues(request: LeagueRequestType!)`, including tier, future/ended/live, date, ordering, `take`, and `skip`.
- Name query is not native and is a bounded client-side scan over authenticated pages.
- Status is derived only from verified future/ended/live values and dates. An incomplete text scan produces a warning and continuation cursor.

## 6. `stratz_query_constants`

### Types mode

The verified `ConstantQuery` exposes bounded aggregates for `heroes`, `items`, `abilities`, game modes, regions, and game versions. The v2 `types` array makes multi-class retrieval explicit. There is no `all` shortcut and no native rank class, so neither is advertised.

### Typed-selectors mode

Each selector names one supported class and a bounded list of IDs. Hero, item, and ability selectors map to their typed constant fields; game-mode, region, and game-version selectors use their corresponding bounded constant mappings. The implementation may load a bounded class aggregate once and select locally, keeping the five-attempt budget. Type and selector errors fail the complete request. Items carry their class in the normalized `type` field.

## 7. Shared feasibility guarantees

- Normalization discards unknown upstream fields and never fabricates unavailable values.
- Exact selector calls are atomic, preserve order and duplicates, and accept at most 25 identifiers.
- Every paginated query uses authenticated, filter-bound cursors and returns `page`; old cursors are invalid after the v2 boundary.
- Five upstream attempts is a hard per-call budget, including bounded scans and local-enrichment fetches.
- Privacy, cache, stale-fallback, response-size, and redaction rules remain those in the normative contract and architecture specification.
- Static resources are unchanged.

## 8. Generation follow-up

The subsequent implementation work must pull the full schema locally, generate selected operations, validate each public field against a concrete selection, define enum mapping tables, and add fixtures for null, private, unparsed, partial, and schema-drift responses. Generated outputs are deliberately not changed by this contract task.
