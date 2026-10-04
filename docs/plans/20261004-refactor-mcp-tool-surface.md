# Refactor the MCP Tool Surface

## Overview

Replace thirteen overlapping curated retrieval tools with five query-oriented tools while retaining `stratz_server_info` and `stratz_execute_graphql`. The final MCP catalog will contain seven tools, use short discovery descriptions, expose explicit query modes, make supported related statistics opt-in, and preserve bounded execution, privacy, cache, pagination, and normalization guarantees.

This is an intentional breaking contract cutover. Superseded tools will not remain as advertised aliases because that would preserve the discovery/context cost this work is intended to remove.

## Source Spec

- Spec: `docs/backlog.md` — `MCP tool-surface refactor`
- Product decision: breaking cutover selected by the user; no temporary compatibility aliases
- Status: Approved for planning
- Last reviewed: 2026-10-04

## Repository Context

- `docs/tool-contracts.json` — canonical tool names, descriptions, JSON Schemas, shared models, and contract version.
- `docs/tool-contracts.md` — normative behavior, examples, compatibility policy, and migration notes.
- `docs/architecture-spec.md` — frozen v1 surface, transport invariants, cursor requirements, security rules, and schema-stability policy.
- `internal/contractgen/generator.go` — validates the exact catalog/version and generates Go contracts, schemas, examples, protocol fixtures, manifests, and reference documentation.
- `internal/contracts/` and `docs/generated-tool-contracts.md` — generated contract artifacts; never edit directly.
- `internal/domain/heroconstants/` — hero resolution, constants loading, bounded hero statistics, and local constants caching.
- `internal/domain/playermatch/` — player and match lookup, exact batches, player history, identifier normalization, and match-detail mapping.
- `internal/domain/leaguelive/` — league lookup/search, league history, live-match filtering, and bounded scans.
- `internal/domain/batch/` — exact-batch deduplication and reconstruction preserving input order and duplicates.
- `internal/domain/pagination/` — authenticated cursors bound to tool, filters, page size, token, schema, and operation version.
- `internal/mcp/` — thin MCP registration/handler adapters, generated-schema validation, cache wrapping, and result envelopes.
- `internal/graphql/operations/*.graphql` and `internal/graphql/schema/bootstrap.graphql` — canonical GraphQL operations and development schema.
- `workflows/workflows.json` — canonical prompts/skills workflow source containing current tool references.
- `integration/live_test.go` — dynamic live coverage for every public tool and approved GraphQL operation.
- `scripts/interop-smoke.sh` — Codex/Claude discovery and schema checks for modern and legacy MCP profiles.

## Implementation Constraints

- Change canonical sources first. Run `make generate` after changes to `docs/tool-contracts.json`, GraphQL operations/schema, or `workflows/workflows.json`; do not hand-edit generated outputs.
- Keep stdout JSON-RPC-only, retain centralized secret redaction, and keep the production STRATZ endpoint fixed.
- Keep MCP handlers as adapters. Put identifier normalization, mode validation, pagination, bounded scans, and upstream mapping in domain packages.
- Preserve the maximum of 25 exact identifiers, five upstream attempts per MCP call, atomic exact-query failure, caller order, and duplicate reconstruction.
- Do not claim unsupported player search, hero dimensions, constant classes, statistics, or filters. Return `INVALID_ARGUMENT`, an explicit unavailable value, or a warning according to the contract.
- Continue supporting MCP `2026-07-28` and legacy `2025-11-25` client profiles with equivalent tool catalogs and generated schemas.
- Keep static `stratz://constants/...` resources unchanged; `stratz_query_constants` changes only the tool surface.
- Maintain signed cursor lifetimes: live 5 minutes, recent 1 hour, and historical 24 hours.
- Cache and stale-fallback policy must be selected from validated query arguments, never from untrusted raw input or only the consolidated tool name.
- Any curated-operation, decoder, enum, filter, or mapping change requires semantic tests and `make test-live`.

## Assumptions

- The contract version becomes `2.0.0-draft.1`, reflecting the deliberate breaking change from `1.0.0-draft.4`; tool names do not carry version suffixes.
- The final catalog is exactly: `stratz_server_info`, `stratz_query_players`, `stratz_query_matches`, `stratz_query_heroes`, `stratz_query_leagues`, `stratz_query_constants`, and `stratz_execute_graphql`.
- All five query tools return a success envelope whose `data` contains a required mode discriminator and an `items` array. Paginated modes additionally return `page`; exact modes omit pagination.
- Exact-selector modes accept 1–25 identifiers, preserve caller order and duplicates, and fail atomically when any selector is invalid, ambiguous, unavailable, or missing.
- `stratz_query_players` supports exact identifiers only because the approved STRATZ roots do not provide a bounded general player directory/search operation.
- `stratz_query_matches` has four explicit modes: `exact`, `player_history`, `league_history`, and `live`. Its output is a discriminator-based union; it never mixes untagged historical, player-history, league-history, and live records.
- `stratz_query_heroes` has exact-selector and bounded local-search modes. Supported hero statistics are an optional request object and are absent by default. Unsupported patch/lane/matchup/synergy dimensions remain rejected until a correct bounded mapping exists.
- `stratz_query_players` moves `match_count` and `win_count` into an optional statistics object. Other tools do not invent statistics that the current curated surface cannot source honestly.
- `stratz_query_leagues` has exact-ID and bounded search modes. `stratz_query_constants` has mutually exclusive `types` and typed `selectors` modes; each selected constant is qualified by constant type to prevent ID/name collisions.
- Discovery descriptions are one sentence and at most 96 UTF-8 bytes each. Detailed usage and migration guidance belongs in contract/reference documentation and generated workflows.
- Old tool-bound cursors become invalid and old cache keys cold-start after cutover. Existing cache rows remain eligible for normal eviction or explicit clearing; no cache migration is added.

## Non-goals

- Preserving the thirteen superseded tool names as aliases.
- Adding a general player text search without an approved bounded STRATZ source.
- Expanding raw GraphQL roots or weakening GraphQL demand controls.
- Adding unsupported hero patch, lane, matchup, synergy, pick-rate, or ban-rate semantics.
- Renaming internal GraphQL operations solely to mirror public MCP tool names.
- Changing static MCP resources, transport protocols, authentication, distribution, or the production STRATZ endpoint.

## Task Summary

1. Define the breaking v2 query contracts and migration semantics.
2. Regenerate and validate the seven-tool contract registry.
3. Implement hero and constants query semantics.
4. Implement player and league query semantics.
5. Consolidate exact, historical, and live match queries.
6. Cut over MCP handlers and request-aware caching.
7. Update workflows and current user-facing documentation.
8. Complete live, interoperability, race, and public-readiness verification.

## Implementation Tasks

### Task 1: Define the breaking v2 query contracts

Goal: Make the complete seven-tool behavior normative before transport and domain code are cut over.

Context:
- `docs/tool-contracts.json` currently defines 15 tools at `1.0.0-draft.4`.
- `docs/architecture-spec.md` freezes v1 names and requires semantic versioning plus migration documentation for breaking changes.
- Consolidated tools need mode-specific validation; optional fields alone must not permit ambiguous mixed requests.

Files:
- Modify: `docs/tool-contracts.json` — canonical v2 names, descriptions, inputs, outputs, and shared models.
- Modify: `docs/tool-contracts.md` — normative query semantics, examples, and v2 migration table.
- Modify: `docs/architecture-spec.md` — replace the frozen v1 catalog and document the intentional v2 boundary.
- Modify: `docs/stratz-schema-feasibility.md` — map each new mode to native or bounded local behavior.

Steps:
- [x] Bump `contractVersion` to `2.0.0-draft.1` and replace the thirteen affected definitions with the five `stratz_query_*` definitions while retaining server info and guarded raw GraphQL.
- [x] Use explicit `mode` constants and `oneOf` branches with `additionalProperties: false`; reject mixed selectors/filters from different modes at schema validation.
- [x] Define `stratz_query_heroes` exact and search modes, typed per-item optional statistics, supported date/rank/role filters, and default-off statistics behavior.
- [x] Define `stratz_query_players` exact mode, normalized identifiers, optional profile statistics, and no unsupported general search mode.
- [x] Define `stratz_query_leagues` exact and search modes, including existing status/tier/date filters and authenticated pagination for search.
- [x] Define `stratz_query_matches` exact, player-history, league-history, and live modes as a discriminator-based input/output union with mode-appropriate details and filters.
- [x] Define `stratz_query_constants` `types` and typed `selectors` modes; remove the ambiguous `all` shortcut because a types array expresses multi-type retrieval.
- [x] Standardize `data.mode`, `data.items`, optional `data.page`, warnings, provenance, and exact-selector atomicity across the new tools without forcing heterogeneous item shapes into one branch.
- [x] Shorten every discovery description to one sentence of at most 96 UTF-8 bytes and keep operational guidance in Markdown/workflows.
- [x] Add an old-to-new migration table covering argument translation, response-shape changes, statistics opt-in, removed names, invalidated cursors, and cache cold starts.

Verification:
- `python3 -m json.tool docs/tool-contracts.json >/dev/null`
- Manually verify every query input branch has a required discriminator and cannot validate against multiple modes.
- Manually verify every public input/output object is closed with `additionalProperties: false` where the existing contract requires it.

Completion criteria:
- The contract alone determines routing, exact-query semantics, pagination, detail availability, statistics behavior, and errors for every mode.
- The migration note explicitly states that no old tool alias or old cursor remains valid.
- No public field promises data that the committed GraphQL operations or bounded local mappings cannot supply honestly.

### Task 2: Regenerate and validate the seven-tool contract registry

Goal: Make generated definitions, fixtures, and documentation authoritative for the v2 catalog.

Context:
- `internal/contractgen/generator.go` hard-codes the contract version and exact tool names.
- Tests currently hard-code 15 definitions, 30 schemas, and 78 generated artifacts.
- Generated Go types removed by this task will require the domain and MCP cutover in subsequent tasks.

Files:
- Modify: `internal/contractgen/generator.go` — expected v2 contract and seven names; description-length validation.
- Modify: `internal/contractgen/generator_test.go` — exact catalog, artifact derivation, schema validation, and stale-file removal.
- Modify: `internal/contracts/contracts_test.go` — generated registry and schema behavior.
- Generated: `internal/contracts/zz_generated.contracts.go`.
- Generated: `internal/contracts/generated/`.
- Generated: `docs/generated-tool-contracts.md`.

Steps:
- [x] Replace `expectedContract` and `expectedTools` with the approved v2 values and exact seven-name catalog.
- [x] Enforce the 96-byte description limit in registry validation and test that the published descriptions equal the canonical source.
- [x] Replace fragile literal artifact-count assertions with a formula based on the exact expected catalog while retaining checks for every required artifact class.
- [x] Add generated-schema tests for each mode, invalid mixed-mode inputs, 25-item bounds, statistics default-off behavior, typed constant selectors, and tagged match outputs.
- [x] Run generation and verify obsolete schemas/examples/protocol fixtures for removed tools are deleted rather than left untracked.
- [x] Record the generated Go type names used by Tasks 3–6 in those implementations rather than adding handwritten duplicate contract types.

Verification:
- `make generate`
- `go test ./internal/contractgen ./internal/contracts`
- `make check-generated`

Completion criteria:
- `contracts.Definitions()` returns exactly the seven approved tools in deterministic order.
- Generated schemas, examples, protocol fixtures, manifest, Go registry, and reference documentation contain no superseded tool names.
- A second `make generate` produces no diff.

### Task 3: Implement hero and constants query semantics

Goal: Consolidate hero lookup/search/statistics and constants retrieval behind bounded domain operations.

Context:
- `internal/domain/heroconstants/service.go` currently separates `FetchHero`, `BatchHeroes`, `FetchHeroStats`, and `FetchConstants`.
- Hero and constant lookup is local over a cached constants aggregate; hero statistics use separate day/week/month operations and a shared five-attempt request budget.
- Rank constants may be unavailable; pick/ban rates are not derivable from win aggregates.

Files:
- Modify: `internal/domain/heroconstants/types.go` — query inputs/results and typed constant selections.
- Modify: `internal/domain/heroconstants/service.go` — exact/search routing, local pagination, multi-type constants, and optional statistics.
- Modify: `internal/domain/heroconstants/service_test.go` — semantic and budget coverage.
- Modify if needed: `internal/graphql/operations/hero_constants.graphql` — bounded multi-hero statistics selection.
- Modify if needed: `internal/graphql/schema/bootstrap.graphql` — only for newly selected upstream fields/arguments.
- Generated if GraphQL changes: `internal/graphql/generated/operations.go` and `operations.json`.

Steps:
- [x] Add domain-owned `QueryHeroes` and `QueryConstants` operations that accept already-validated generated request types but perform normalization, ambiguity checks, bounds, and upstream mapping in the domain package.
- [x] Reuse hero indexing and `domain/batch` reconstruction so exact mode remains atomic, ordered, duplicate-preserving, and capped at 25.
- [x] Add deterministic local hero search filters and authenticated cursor state over the cached constants set; bind mode, effective filters, page size, token namespace, schema version, and operation version.
- [x] Fetch statistics only when the optional statistics object is present, use one shared request budget, aggregate only `[from,to)`, and key results by resolved `hero_id`.
- [x] Keep pick/ban rates explicitly unavailable with warnings and reject unsupported patch/lane/matchup/synergy fields rather than fabricating values.
- [x] Support one or multiple constant types and typed `(type,id|name)` selectors; preserve deterministic sorting and the existing 20,000-record response guard.
- [x] Preserve constants-cache `singleflight`, stale behavior, sanitization, and warning provenance.

Verification:
- `go test ./internal/domain/heroconstants ./internal/domain/pagination ./internal/domain/batch`
- If GraphQL changes: `make generate && make check-generated`

Completion criteria:
- Default hero queries perform no statistics GraphQL operation and omit statistics from items.
- Exact hero queries preserve current identifier, ambiguity, ordering, duplicate, and atomic-failure behavior.
- Multi-type and selected-constant queries cannot collide across constant classes and have semantic tests for non-empty representative values.

### Task 4: Implement player and league query semantics

Goal: Consolidate supported player and league retrieval without exposing unbounded or unsupported search behavior.

Context:
- Players support exact identifiers and native plural retrieval but no approved general directory search.
- League search combines native filters with bounded client-side text scanning and authenticated cursors.
- Existing player results always include `match_count` and `win_count`; v2 makes that related statistics payload opt-in.

Files:
- Modify: `internal/domain/playermatch/types.go` and `service.go` — exact player query and optional statistics mapping.
- Modify: `internal/domain/playermatch/service_test.go` — identifiers, atomicity, and statistics tests.
- Modify: `internal/domain/leaguelive/types.go` and `service.go` — exact/search league query modes.
- Modify: `internal/domain/leaguelive/service_test.go` — native/local filters and cursor tests.
- Modify if needed: `internal/graphql/operations/player.graphql` and `league_live.graphql` — avoid fetching optional statistics when omitted where the upstream schema permits.
- Modify if needed: `internal/graphql/schema/bootstrap.graphql` and generated GraphQL outputs.

Steps:
- [x] Add an exact-only player query using existing account-ID, SteamID64, and STRATZ URL normalization plus native plural retrieval.
- [x] Keep player exact queries capped at 25, atomic, ordered, and duplicate-preserving; reject search-like fields not backed by an approved source.
- [x] Move `match_count` and `win_count` into an optional per-player statistics object and ensure they are absent by default; use a lean GraphQL selection when feasible rather than only discarding fetched fields.
- [x] Add league exact-ID and bounded search modes, reusing upstream-native ID/status/tier/date filters and the existing bounded local text scan.
- [x] Bind league search cursors to the new tool, `mode`, all effective filters, page size, token, schema version, and operation version.
- [x] Preserve deterministic incomplete-scan warnings, sanitization, provenance, and exact-ID `NOT_FOUND` versus empty search-page behavior.

Verification:
- `go test ./internal/domain/playermatch ./internal/domain/leaguelive ./internal/domain/pagination ./internal/domain/batch`
- If GraphQL changes: `make generate && make check-generated`

Completion criteria:
- Player requests cannot trigger an unbounded directory scan and return no statistics unless explicitly requested.
- League exact and search modes retain existing native/local filtering semantics and cursor resumability.
- Tests assert representative non-zero player counters when statistics are requested, not merely operation names or schema shape.

### Task 5: Consolidate exact, historical, and live match queries

Goal: Route four explicit match modes through existing domain owners while preserving their distinct item shapes, filters, detail levels, and cursor lifetimes.

Context:
- Exact matches and player history live in `playermatch`; league history and live matches live in `leaguelive`.
- Exact batches use aliased single-match roots because the plural match endpoint may be admin-only.
- Player history, league history, and live search have materially different traversal and filtering behavior.

Files:
- Modify: `internal/domain/playermatch/service.go`, `types.go`, and tests — exact and player-history v2 inputs/results.
- Modify: `internal/domain/leaguelive/service.go`, `types.go`, and tests — league-history and live v2 inputs/results.
- Modify: `internal/domain/pagination/cursor_test.go` and `scan_test.go` — mode/tool/filter binding and lifetimes.
- Modify if needed: `internal/graphql/operations/match.graphql` and `league_live.graphql`.
- Modify if needed: `internal/graphql/schema/bootstrap.graphql` and generated GraphQL outputs.

Steps:
- [x] Map `exact` mode to existing summary/standard/full match operations and batch chunking while retaining five-attempt limits, detail availability errors, order, duplicates, and atomic failure.
- [x] Map `player_history` mode to native player/date/hero/role/game-mode/lobby/result/patch filters plus bounded minimum-duration filtering and optional player rows.
- [x] Map `league_history` mode to direct offset pagination and existing league/date/patch/detail semantics.
- [x] Map `live` mode to native league/hero/state/tier/order filters plus bounded player/team/game-mode/minimum-spectator filtering.
- [x] Return a tagged output branch per mode (`Match`, `PlayerMatchSummary`, `MatchSummary`, or `LiveMatch`) and reject detail/filter combinations that do not apply to the selected mode.
- [x] Change every pagination binding to `stratz_query_matches` and include `mode` in the canonical filter hash; increment operation versions where traversal state changes.
- [x] Add adversarial tests proving cursors cannot cross player-history, league-history, and live modes and old tool-name cursors fail with `CURSOR_INVALID`.
- [x] Preserve live 5-minute, recent 1-hour, and historical 24-hour lifetimes and the warning that live cursors are not snapshots.

Verification:
- `go test ./internal/domain/playermatch ./internal/domain/leaguelive ./internal/domain/pagination ./internal/domain/batch`
- If GraphQL changes: `make generate && make check-generated`

Completion criteria:
- Every match mode uses bounded existing upstream behavior and returns only its declared item type.
- No cursor can be replayed against another mode, filter set, page size, token namespace, schema, or operation version.
- Tests include non-zero counters and non-empty player/objective/timeline/live rows where the selected detail level promises them.

### Task 6: Cut over MCP handlers and request-aware caching

Goal: Advertise and serve only the seven-tool v2 catalog with cache/privacy behavior derived from validated query mode and options.

Context:
- `internal/mcp/server.go` advertises every generated definition even if its handler is missing.
- `internal/mcp/cache.go` currently assigns one fixed domain/class per tool name, which is insufficient for consolidated modes.
- `stratz_query_matches` spans profile-sensitive, public-recent, and public-live data; hero statistics change reference data to public-recent.

Files:
- Modify: `internal/mcp/hero_constants.go` and tests — query hero/constants adapters.
- Modify: `internal/mcp/player_match.go` and tests — query player/match adapters and mode dispatch.
- Modify: `internal/mcp/league_live.go` and tests — query league and match-mode adapters.
- Modify: `internal/mcp/cache.go` and `cache_test.go` — argument-aware classification and detail policy.
- Modify: `internal/mcp/server.go`, `server_test.go`, and `result.go` if required — exact registration and v2 operation labels.
- Modify: `internal/cache/key.go` tests if canonical query-mode key coverage is missing.

Steps:
- [x] Register exactly one handler for every generated query definition and remove all superseded registrations and operation labels.
- [x] Keep adapters thin: decode generated input, select the owning domain method by validated mode, share one request budget where enrichment needs it, and pass results through centralized envelope/schema validation.
- [x] Replace the fixed cache map with a resolver over validated arguments: heroes are `PublicReference` or `PublicRecent` with statistics; players are `ProfileSensitive`; leagues are `PublicReference` for exact and `PublicRecent` for search; matches are `PublicRecent`, `ProfileSensitive`, or `PublicLive` by mode; constants are `PublicReference`.
- [x] Fail closed to the stricter/shorter-lived classification when a future valid combination spans classes, and preserve `include_raw` cache bypass and token namespaces.
- [x] Make detail-level authorization mode-aware so player rows are accepted only for applicable match modes.
- [x] Update server-info catalog/version metadata and assertions for the seven-tool v2 surface.
- [x] Add `tools/list` tests for exact names, generated schemas, 96-byte descriptions, and absence of old names under both preferred and legacy protocol lifecycles.
- [x] Test cache keys and stale fallback separately for each mode, statistics setting, detail level, filters, cursor, and privacy class.

Verification:
- `go test ./internal/mcp ./internal/cache`
- `go test -race ./internal/mcp ./internal/cache ./internal/domain/...`

Completion criteria:
- Every advertised definition has exactly one callable handler and no removed name is discoverable or callable.
- Cache TTL, privacy, stale fallback, and raw bypass follow validated request semantics.
- Output validation and centralized redaction remain active for every new mode.

### Task 7: Update workflows and current user-facing documentation

Goal: Teach clients the consolidated surface without putting verbose usage guidance back into discovery descriptions.

Context:
- `workflows/workflows.json` is canonical and currently references old tool names; generated prompts and skills must not be edited directly.
- `README.md` reports a 15-tool surface, and current architecture/cache/reference docs describe per-tool classifications and old names.

Files:
- Modify: `workflows/workflows.json` — new tool names, modes, arguments, and explicit statistics requests.
- Modify: `internal/workflowgen/generator_test.go` and `internal/prompts/prompts_test.go` — catalog-reference and prompt-safety coverage.
- Generated: `internal/prompts/zz_generated.prompts.go`, `skills/*/SKILL.md`, and `docs/skills-installation.md`.
- Modify: `README.md` — seven-tool overview and migration pointer.
- Modify: `docs/cache.md` — request-aware classification.
- Modify: `docs/interoperability.md` — v2 discovery expectations.
- Modify: `docs/resources-prompts-skills.md` — query-oriented workflow guidance.
- Modify: `docs/troubleshooting.md` — old cursor invalidation and cache cold-start guidance.

Steps:
- [x] Rewrite all canonical workflows to use query modes, requesting hero/player statistics only when the workflow consumes them.
- [x] Add workflow validation that every referenced `stratz_*` tool exists in `contracts.Definitions()` so stale names fail generation/tests.
- [x] Regenerate prompts, skills, and installation documentation; preserve prompt-injection defenses and treatment of STRATZ strings as untrusted data.
- [x] Replace active documentation references to the 15-tool catalog and old invocations while leaving completed historical plans unchanged.
- [x] Document request-aware cache classes, invalidated v1 cursors, cold cache behavior, and the lack of compatibility aliases.
- [x] Keep static constants resource URIs and raw GraphQL guidance unchanged except where examples refer to removed tools.

Verification:
- `make generate`
- `go test ./internal/workflowgen ./internal/prompts`
- `make check-generated`
- Search current docs/workflows/generated skills for superseded names; matches must be limited to the v2 migration table or explicitly historical documents.

Completion criteria:
- No active workflow, generated skill, README example, or current operational guide instructs clients to call a removed tool.
- Detailed mode/filter guidance is available outside MCP discovery descriptions.
- Workflow generation fails if a future workflow references a nonexistent tool.

### Task 8: Complete live, interoperability, and release-gate verification

Goal: Prove the breaking surface change works against STRATZ and both supported client profiles without stale generated artifacts or semantic regressions.

Context:
- `integration/live_test.go` currently invokes every old public tool and tracks exact GraphQL operation coverage.
- `scripts/interop-smoke.sh` already compares `tools/list` against generated contracts for modern and legacy lifecycles.
- Project guidance requires live tests for curated-operation/filter/mapping changes and both Codex and Claude smoke profiles for MCP changes.

Files:
- Modify: `integration/live_test.go` — every query mode, opt-in statistics, and current operation coverage.
- Modify if needed: `scripts/interop-smoke.sh` — explicit v2 catalog/description assertions beyond generated schema comparison.
- Modify if needed: `.github/workflows/foundation.yml`, `container.yml`, and `release.yml` only when existing commands do not exercise the new checks.

Steps:
- [x] Replace old live calls with exact and paginated coverage for all five query tools and every supported mode.
- [x] Assert hero/player statistics are absent by default and carry representative non-zero semantic values when requested.
- [x] Cover constants single-type, multi-type, and typed-selector requests and all retained approved GraphQL operations.
- [x] Probe playback detail one match at a time and retain semantic assertions for non-empty objectives/timeline/player rows.
- [x] Verify Codex and Claude profiles discover the same exact seven-tool catalog and schemas under modern `2026-07-28` and legacy `2025-11-25` lifecycles.
- [x] Run generation twice, the full race suite, public/restricted-source checks, notices, and live tests; resolve every stale artifact or semantic mismatch.

Verification:
- `make generate && make check-generated`
- `make check`
- `go test -race ./...`
- `CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp`
- `CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp`
- `make test-live`
- `make public-readiness`
- `make check-restricted`
- `make notices`

Completion criteria:
- All repository checks, race tests, both client-profile smoke tests, and live tests pass.
- A second generation run is clean and no restricted schema, token, cache database, introspection payload, or fetched constants are tracked.
- Live tests prove normalized meaning for each query mode rather than only validating schemas, operation names, or non-error responses.

## Cross-Task Verification

- `make generate && make check-generated`
- `make check`
- `go test -race ./...`
- `CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp`
- `CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp`
- `make test-live`
- `make public-readiness && make check-restricted && make notices`
- `git status --short` shows only intended source and deterministic generated changes.

## Risks and Mitigations

- Risk: Breaking removal strands clients using old names or cursors.
  Mitigation: Use a `2.0.0-draft.1` contract boundary, publish a complete migration table, fail old cursors explicitly, and keep old names out of discovery rather than offering misleading partial compatibility.
- Risk: One match tool accidentally merges incompatible record shapes or filters.
  Mitigation: Require mode-discriminated input and output unions and adversarial mixed-mode/cross-cursor tests.
- Risk: Consolidated tools weaken cache privacy by applying one public class to sensitive modes.
  Mitigation: Resolve classification only after schema validation, map each mode explicitly, and fail closed to the strictest class.
- Risk: Optional multi-hero statistics exceed the five-attempt budget or lose hero identity.
  Mitigation: Use bounded aggregate operations with explicit hero IDs, one shared budget, semantic fixtures, and live validation; reject unsupported combinations rather than looping per hero.
- Risk: Local hero/league scans make cursors unstable across constants/schema changes.
  Mitigation: Sort deterministically and bind cursors to mode, filters, page size, token namespace, schema, and operation version.
- Risk: Generated types disappear before all packages are adapted.
  Mitigation: Execute Tasks 2–6 consecutively on one branch, use focused package tests during the transition, and require the full build at Task 6 completion.
- Risk: Smaller tool count still yields a large discovery payload because consolidated schemas grow.
  Mitigation: Enforce terse descriptions, reuse canonical `$defs` before dereferencing, measure `tools/list` in conformance tests, and reject schema duplication that erases the intended context reduction.

## Open Questions

- None. The breaking-cutover decision and remaining contract choices are recorded above as implementation assumptions.
