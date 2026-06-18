---
Created: 2026-06-18
Purpose: Record live STRATZ schema evidence and assess feasibility of the proposed curated MCP tools.
Status: Core v1 domains verified; exact operation selections and normalized field mappings remain to be generated from the full local schema
---

# STRATZ schema feasibility

## 1. Method

Authenticated GraphQL introspection was run against:

```text
https://api.stratz.com/graphql
```

Only root signatures and selected public type/input signatures were printed. The full fetched schema was not committed or redistributed.

Developer commands:

- `cmd/stratz-schema-inspect`
- `cmd/stratz-discovery`

## 2. Root API

The live `DotaQuery` root exposes:

| Field | Signature |
|---|---|
| `constants` | `ConstantQuery` |
| `heroStats` | `HeroStatsQuery` |
| `league` | `(id: Int!) -> LeagueType` |
| `leagues` | `(request: LeagueRequestType!) -> [LeagueType]` |
| `live` | `LiveQuery` |
| `match` | `(id: Long!) -> MatchType` |
| `matches` | `(ids: [Long]!) -> [MatchType]` |
| `player` | `(steamAccountId: Long!) -> PlayerType` |
| `players` | `(steamAccountIds: [Long]!) -> [PlayerType]` |
| `team` | `(teamId: Int!) -> TeamType` |
| `teams` | `(teamIds: [Int]!) -> [TeamType]` |

Other roots include guild, leaderboard, plus, STRATZ, vendor, and yogurt domains.

The approved raw policy allows `guild` and `leaderboard`. It denies `plus`, `stratz`, `vendor`, and `yogurt` pending separate security/data review. Query operation type alone does not grant access, and new upstream roots remain denied until an explicit contract revision.

## 3. Batch feasibility

### Matches

`matches(ids: [Long]!)` natively supports one-request match batches. The v1 25-item batch limit is feasible within the five-request budget.

### Players

`players(steamAccountIds: [Long]!)` natively supports one-request player batches. Identifier normalization must happen before constructing this request.

### Heroes

There is no `heroes(ids: ...)` field. `constants.heroes(...)` returns the hero set, and `constants.hero(id: ...)` returns one hero.

The batch tool remains feasible by loading/caching the bounded hero constant set once and selecting requested IDs locally. Name and slug resolution is also local and must reject ambiguity.

## 4. Player tools

`PlayerType` exposes:

- `steamAccountId`
- `steamAccount`
- `identity`
- `simpleSummary`
- `ranks`
- `leaderboardRanks`
- `matchCount`
- `winCount`
- `firstMatchDate`
- `lastMatchDate`
- `matches(request: PlayerMatchesRequestType!)`
- performance and hero-performance fields

`PlayerMatchesRequestType` supports:

- `startDateTime`, `endDateTime`
- `heroIds`
- `positionIds`, `roleIds`, `laneIds`
- `gameModeIds`
- `lobbyTypeIds`
- `isVictory`
- `gameVersionIds`
- `leagueId`, `leagueIds`
- `regionIds`
- `isParsed`, `isStats`, `isParty`, `isRadiant`
- team, friend, and enemy filters
- `take`, `skip`, `before`, `after`

Implications:

- The proposed date, hero, role, lane, game-mode, lobby, win/loss, region, league, and patch/game-version filters are feasible.
- Minimum duration is not a native filter. It requires bounded client-side filtering.
- Opaque MCP cursors must preserve STRATZ `before`/`after` or skip state plus client-side scan progress.

## 5. Match tools

`MatchType` exposes the core normalized match fields:

- ID and timestamps.
- Duration and result.
- Game mode, lobby type, region, game version, league, and series.
- Players.
- Pick/bans.
- Team and lane outcomes.
- Kill, net-worth, and experience timelines.
- Tower/building events.
- Playback data.

`MatchPlayerType` exposes:

- Steam account and hero IDs.
- Team side, slot, role, position, and lane.
- Kills, deaths, assists, level, net worth, GPM, XPM, damage, healing, and tower damage.
- Items, abilities, stats, and player playback data.

`MatchPlaybackDataType` exposes bounded event groups for:

- Buildings.
- Couriers.
- Roshan.
- Runes.
- Towers.
- Wards.

Implications:

- `summary`, `standard`, and `full` match levels are feasible.
- The exact normalized fight/economy mappings need generated operations and fixture validation.
- Missing match ID `1` returned HTTP 200 with `data.match: null`; curated tools map this to `NOT_FOUND`.

## 6. Constants and heroes

`ConstantQuery` exposes:

- `hero` and `heroes`.
- `item` and `items`.
- `ability` and `abilities`.
- Game modes and lobby types.
- Regions.
- Roles.
- Game versions.
- Facets, NPCs, modifiers, patch notes, and other reference data.

Implications:

- Hero, item, ability, game-mode, region, and game-version resources are feasible.
- Rank constants are not a `ConstantQuery` field. Rank metadata must be derived from committed schema enums/reference mappings or removed from `stratz_get_constants` if redistribution terms do not allow that mapping.
- Hero lookup by localized name/slug is a local index over `constants.heroes`.

## 7. Hero statistics

`HeroStatsQuery` exposes:

- `stats` with hero IDs, week, rank brackets, positions, and time grouping.
- `winDay`, `winWeek`, `winMonth`, `winHour`, and `winGameVersion`.
- `banDay`.
- `heroVsHeroMatchup` and `matchUp`.
- Lane outcomes.
- Item, ability, talent, and guide statistics.

`HeroPositionTimeDetailType` includes match/win counts, hero ID, week/time, position/rank grouping, and many aggregate performance fields.

Implications:

- Win rate, sample size, role/position breakdown, time trends, matchup, and synergy analysis are feasible.
- Pick rate requires a denominator query across the same population.
- Ban rate uses a separate time-bucketed operation.
- Arbitrary RFC 3339 date ranges are not a single native argument. The curated tool must translate them into bounded day/week/month buckets, document the effective range in provenance, and warn when the range is rounded.
- A patch/game-version filter is naturally supported by `winGameVersion`, but not every metric supports the same game-version dimension. Unsupported combinations return `INVALID_ARGUMENT` or explicit `null` fields with a warning; they must not silently mix populations.

## 8. Leagues

`LeagueRequestType` supports:

- League ID(s).
- Start/end and between-date filters.
- Future/ended/live flags.
- Tier.
- Ordering.
- Image/prize/date requirements.
- `take` and `skip`.

`LeagueType` exposes:

- ID, name/display name, description, region, country, tier, dates, image, prize pool, venue, streams, and live status.
- `matches(request: LeagueMatchesRequestType!)`.
- Series, standings, tables, stages, and statistics.

`LeagueMatchesRequestType` supports date, game mode/version, hero, parsed/stats, lane/role/position, lobby, rank, region, series, team/player, and pagination filters.

Implications:

- League retrieval and league-match listing are feasible.
- Normalized `status` is derived from dates plus future/ended/live fields.
- Text name search is not a native filter. It uses bounded client-side scanning over paginated league results. Incomplete scans must return a warning and continuation cursor rather than claiming exhaustive search.

## 9. Live matches

`LiveQuery` exposes:

- `match(id:, skipPlaybackDuration:)`.
- `matches(request: MatchLiveRequestType)`.

Native live request filters:

- Game state.
- Hero.
- Completion/parsing state.
- League ID(s).
- League tier.
- Ordering.
- `take` and `skip`.

Native order values:

- `AVERAGE_RANK`
- `GAME_TIME`
- `MATCH_ID`
- `SPECTATOR_COUNT`

`MatchLiveType` exposes:

- Match/lobby IDs.
- Game time/minute, game mode, and game state.
- League and team IDs/objects.
- Players.
- Spectator count.
- Scores and radiant lead.
- Average rank.
- Playback and win-rate data.

It does not expose a region field.

Implications:

- League, hero, state, tier, newest, and spectator ordering are native.
- Team, player, game mode, and minimum spectator filters are feasible through bounded client-side filtering.
- Region filtering is not feasible and has been removed from the v1 curated contract.
- Client-side live filtering may scan at most five upstream pages per MCP call and resumes through the authenticated cursor.

## 10. Required contract-generation work

Before curated implementation:

1. Pull the full schema locally using the verified Go HTTP contract.
2. Generate selected-operation types.
3. Validate every field in `docs/tool-contracts.json` against concrete STRATZ selections.
4. Define exact enum mapping tables for roles, positions, ranks, tiers, modes, lobby types, game states, and order values.
5. Define bounded client-side scan semantics for non-native filters.
6. Add fixtures for null, private, unparsed, partial, and schema-drift responses.
7. Update the schema feasibility status when all normalized fields have an approved source or derivation.
