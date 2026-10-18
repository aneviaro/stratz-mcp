# Fix Player-History Role and Patch Filters for the Current STRATZ Schema

## Overview

Correct `stratz_query_matches` player-history filters for the current STRATZ GraphQL request contract. Public role values become validated semantic names mapped to STRATZ's integer role IDs; position and lane are exposed as separate filters; numeric patch/game-version IDs are validated before any upstream request; and the bootstrap schema/generated operations use integer `roleIds` and `gameVersionIds`.

## Source Spec

- Spec: [GitHub issue #32](https://github.com/aneviaro/stratz-mcp/issues/32)
- Status: Open; implementation plan assumes the issue #31 metadata contract is released in the same draft-v2 contract revision.
- Last reviewed: 2026-10-18

## Repository Context

- `docs/tool-contracts.json` — canonical closed input schema for `stratz_query_matches`; the player-history branch currently accepts arbitrary role strings and unrestricted patch strings.
- `docs/tool-contracts.md` and `docs/architecture-spec.md` — normative contract/version and filter semantics documentation.
- `internal/contractgen/` and `internal/contracts/` — generate and validate public schemas, examples, protocol fixtures, and reference documentation.
- `internal/graphql/schema/bootstrap.graphql` — committed schema used by genqlient; `PlayerMatchesRequestType.roleIds` and `gameVersionIds` are stale string lists and do not include position/lane request fields.
- `internal/graphql/operations/player.graphql` and `league_live.graphql` — curated operations that consume the request input types; no operation selection change is expected, but generated artifacts must be regenerated.
- `internal/domain/playermatch/service.go` — player-history validation, cursor binding, native request construction, and the current string-valued role/patch mappings.
- `internal/mcp/league_live.go` — player-history MCP adapter path; it currently parses role and patch strings independently of the domain validator.
- `internal/domain/leaguelive/service.go` — league-history uses the same stale string `gameVersionIds` request field and should receive the same numeric patch conversion for contract consistency.
- `internal/domain/playermatch/service_test.go`, `internal/domain/leaguelive/service_test.go`, `internal/contracts/contracts_test.go`, and `integration/live_test.go` — fixture, schema-boundary, and live coverage locations.
- `docs/plans/20261018-expose-match-position-role-lane-party-metadata.md` — pending issue #31 plan; its planned `2.0.0-draft.2` contract migration is the version boundary assumed here.

## Implementation Constraints

- Edit canonical sources only. Do not hand-edit `internal/graphql/generated/**`, `internal/contracts/generated/**`, `internal/contracts/zz_generated.contracts.go`, or `docs/generated-tool-contracts.md`.
- Keep MCP handlers as adapters. Shared primitive validation and upstream mapping belong in domain code; hero name resolution remains in the adapter because it requires the existing per-call budget.
- Send `roleIds` as integer IDs: `CORE = 0`, `LIGHT_SUPPORT = 1`, and `HARD_SUPPORT = 2`.
- Send `position` as the upstream enum token (`POSITION_1` through `POSITION_5`). Do not infer position from `player_slot` or combine it with `role`.
- Send `lane` as integer IDs documented by the current schema: `ROAMING = 0`, `SAFE_LANE = 1`, `MID_LANE = 2`, `OFF_LANE = 3`, and `JUNGLE = 4`.
- Treat `patch_id` as a positive decimal STRATZ game-version ID that fits the upstream GraphQL `Int`; reject dotted display versions and non-numeric values before executing the request.
- Preserve normalized/canonical filter values in cursor bindings so pagination cursors cannot be reused across different role, position, lane, or patch filters.
- Do not commit the fetched/restricted STRATZ schema or cache artifacts used to confirm these mappings.

## Assumptions

- Issue #31 and issue #32 are released together under `2.0.0-draft.2`; if issue #31 has already landed as draft.2, this work must not bump the version a second time. If the issues are intentionally released separately, make this plan's input-contract changes the next monotonic draft version and update the generator expectation accordingly.
- Public player-history filter names are `role`, `position`, and `lane`. `role` accepts only `CORE`, `LIGHT_SUPPORT`, and `HARD_SUPPORT`; `position` accepts only `POSITION_1` through `POSITION_5`; `lane` accepts only the five documented lane IDs. `UNKNOWN`, `ALL`, and `FILTERED` are not exposed as filters because the current request contract does not provide a documented numeric mapping for all of them.
- `patch_id` remains a string at the MCP boundary for compatibility with the existing contract shape, but its accepted value is now a canonical decimal integer string. Normalized match output remains unchanged.
- The existing league-history `patch_id` is brought along with the same numeric conversion because it targets the same upstream `gameVersionIds` scalar and would otherwise retain the known protocol failure.

## Non-goals

- Changing normalized match-player output metadata from issue #31, adding filters to exact/live modes, or changing the meaning of output `position`.
- Resolving human-readable patch names such as `7.39` through an additional constants request.
- Adding unsupported role, position, lane, rank, team, or party filters.
- Changing pagination limits, request budgets, cache behavior, retry policy, or the production STRATZ endpoint.
- Committing fetched introspection, constants, schemas, `.env`, tokens, or restricted cache content.

## Task Summary

1. Make role, position, lane, and numeric patch semantics authoritative in the public contract and docs.
2. Synchronize the canonical bootstrap request types and regenerate GraphQL artifacts.
3. Centralize filter validation and map canonical values to current STRATZ request variables in player and league history.
4. Add fixture, schema-boundary, MCP error, and live tests, then run all repository gates.

## Implementation Tasks

### Task 1: Define the public filter contract and versioned migration

Goal: Make valid player-history filter values explicit so unsupported role/patch inputs fail as `INVALID_ARGUMENT` before domain execution or an upstream call.

Context:

- The current JSON schema accepts any string for `role` and `patch_id`, allowing `POSITION_1` and `1` to reach the stale string-valued upstream request.
- `docs/tool-contracts.md` requires a contract-version decision for public input changes. Issue #31 already defines the draft-v2 migration boundary in the pending plan.

Files:

- Modify: `docs/tool-contracts.json` — change the player-history `role` property to the three-value enum; add `position` and `lane` enum properties; constrain `patch_id` to a positive decimal string that fits GraphQL `Int`. Apply the same patch-id constraint to the league-history branch if the shared semantics are adopted.
- Modify: `docs/tool-contracts.md` — document role-to-ID, position enum, lane-to-ID, numeric patch-id, and rejection semantics; record the draft-v2 migration alongside issue #31 rather than silently changing a draft contract.
- Modify: `docs/architecture-spec.md` — update the current contract version/filter semantics and state that invalid filters are rejected before upstream execution.
- Modify: `internal/contractgen/generator.go` — update the expected contract version only to the agreed draft-v2 value after reconciling the issue #31 version change.
- Generated: `internal/contracts/generated/**`, `internal/contracts/zz_generated.contracts.go`, and `docs/generated-tool-contracts.md` — regenerate; do not edit directly.
- Test: `internal/contracts/contracts_test.go` — add valid role/position/lane/patch inputs and invalid `role: POSITION_1`, `role: 1`, dotted patch, zero/negative patch, and unknown enum values.

Steps:

- [x] Define the closed player-history input branch with `role`, `position`, and `lane` as separate enums and with a numeric `patch_id` pattern/length bound.
- [x] Decide and apply the single monotonic contract version with issue #31; keep the generator's expected version and normative docs synchronized.
- [x] Regenerate all contract artifacts and inspect that only the intended input schemas, examples, fixtures, generated Go metadata, and references changed.
- [x] Add schema tests proving accepted values validate and unsupported values are rejected before a handler can run.

Verification:

- `make generate`
- `make check-generated`
- `go test ./internal/contractgen ./internal/contracts`

Completion criteria:

- The published player-history schema advertises only the supported semantic role, position, lane, and numeric patch values.
- Invalid examples from the issue fail schema validation as `INVALID_ARGUMENT` inputs, not as upstream protocol failures.
- The contract version and issue #31 migration documentation have one consistent value.

### Task 2: Synchronize the canonical STRATZ request schema

Goal: Make genqlient's committed bootstrap schema describe the current request variable types used by the curated player-history and league-history operations.

Context:

- The current fetched STRATZ schema defines `PlayerMatchesRequestType.roleIds: [Int]`, `positionIds: [MatchPlayerPositionType]`, `laneIds: [Int]`, and `gameVersionIds: [Int]`; `take` and `skip` are nullable `Int` fields for player history.
- The current fetched league request also uses integer `gameVersionIds` and exposes the same position/lane/role filter families, while the repository bootstrap still has string game-version/role lists.

Files:

- Modify: `internal/graphql/schema/bootstrap.graphql` — update the relevant `PlayerMatchesRequestType` fields to current scalar/enum types, add `positionIds` and `laneIds`, and update `LeagueMatchesRequestType.gameVersionIds` to `[Int]`; add only supporting scalar/enum declarations required by the canonical request types.
- Generated: `internal/graphql/generated/operations.go` and `internal/graphql/generated/operations.json` — regenerate from bootstrap schema and canonical operations.
- Do not modify: `internal/graphql/operations/player.graphql` or `internal/graphql/operations/league_live.graphql` unless generation reveals that a variable/operation source must change; the curated field selections already reference the request input types.
- Test: GraphQL generation/schema tests under `internal/graphql/` — assert generated request types expose integer role/game-version fields and the new position/lane request fields.

Steps:

- [x] Change `roleIds` and `gameVersionIds` from string lists to integer lists in the canonical bootstrap schema.
- [x] Add `positionIds: [MatchPlayerPositionType]` and `laneIds: [Int]` with the current upstream nullability; retain the existing request fields required by the curated operations.
- [x] Keep the bootstrap schema limited to the committed curated contract; do not copy the restricted full schema or unrelated upstream fields into the repository.
- [x] Run GraphQL generation and inspect generated operation text/types for the player and league match operations.

Verification:

- `make generate`
- `make check-generated`
- `go test ./internal/graphql/...`
- Inspect generated `PlayerMatchesRequestType` and `LeagueMatchesRequestType` for integer `roleIds`/`gameVersionIds`, enum `positionIds`, and integer `laneIds`.

Completion criteria:

- Generated curated operations compile against the bootstrap schema and no longer describe the affected filters as string lists.
- The request schema accepts the exact variable shapes emitted by the domain mappings without local schema drift.
- No fetched/restricted schema artifact is added to the repository.

### Task 3: Normalize and map filters before upstream execution

Goal: Validate every player-history filter in the domain layer and emit the exact current STRATZ request variables, including cursor-bound canonical filters.

Context:

- `queryPlayerMatchFilters` validates direct domain calls, but `internal/mcp/league_live.go` currently decodes player-history role and patch strings separately and can bypass the domain validation.
- `nativePlayerMatchRequest` currently sends `roleIds` and `gameVersionIds` as `[]string`, which causes the upstream `Int` conversion failure.

Files:

- Modify: `internal/domain/playermatch/service.go` — allow `position` and `lane`, centralize primitive history-filter parsing/normalization for both `QueryMatches` and the MCP adapter, validate role/position/lane/patch values, and include all normalized filters in `filterBinding`.
- Modify: `internal/domain/playermatch/types.go` — add typed `Position` and `Lane` filter fields (or equivalent normalized representations) without changing normalized match-player output fields from issue #31.
- Modify: `internal/domain/playermatch/service.go` or a new focused `internal/domain/playermatch/filters.go` — map `CORE/LIGHT_SUPPORT/HARD_SUPPORT` to `0/1/2`, position tokens to GraphQL enum strings, lane tokens to `0..4`, and patch strings to validated integer IDs.
- Modify: `internal/mcp/league_live.go` — consume the shared domain parser for role, position, lane, dates, integers, and patch; retain only hero resolution and request-budget orchestration in the adapter.
- Modify: `internal/domain/leaguelive/service.go` and `internal/domain/leaguelive/types.go` — parse league-history patch IDs once and send `gameVersionIds` as integer values, preserving the same invalid-input behavior.
- Test: `internal/domain/playermatch/service_test.go` and `internal/domain/leaguelive/service_test.go` — assert request variables and no-executor validation failures.

Steps:

- [x] Replace the generic role string with explicit canonical validation; `POSITION_1` and numeric `1` supplied as `role` must return a domain `Error` with `INVALID_ARGUMENT` before `fixtureExecutor.Execute` is called.
- [x] Add separate position and lane filter parsing; send `positionIds: []string{...}` and `laneIds: []int64{...}` (or the repository's equivalent JSON-compatible integer type) without deriving one from another.
- [x] Parse `patch_id` with a bounded base-10 integer parser before request construction; reject whitespace-only, dotted, signed, negative, zero, overflow, and non-numeric values with `INVALID_ARGUMENT`.
- [x] Emit `roleIds: []int64{0|1|2}` and `gameVersionIds: []int64{parsedID}` for player history, and the integer game-version list for league history.
- [x] Canonicalize filter values in the cursor binding so changing any of role, position, lane, or patch invalidates an existing cursor rather than silently reusing it.
- [x] Preserve all existing date, hero, game-mode, lobby, result, minimum-duration, pagination, and request-budget behavior.

Verification:

- `go test ./internal/domain/playermatch ./internal/domain/leaguelive`
- Assert the fixture request map contains integer arrays and that invalid values leave the executor call count at zero.
- Confirm the MCP adapter maps domain invalid errors to the stable `INVALID_ARGUMENT` execution envelope.

Completion criteria:

- A valid player-history request reaches the executor with integer role/game-version IDs plus independent position/lane filters.
- Unsupported values are rejected locally and never become `UPSTREAM_PROTOCOL_ERROR`.
- Player and league history pagination cursors include the effective filter values.

Implementation note: live validation against the current STRATZ endpoint showed that requests containing `roleIds` or `laneIds` are rejected as `INVALID_OPERATION`. To avoid exposing an upstream protocol failure or silently ignoring a public filter, valid role and lane values are now rejected during domain normalization with `INVALID_ARGUMENT` until STRATZ accepts those fields; the closed enums and mappings remain documented and covered for the upstream compatibility correction.

### Task 4: Add semantic fixture, MCP boundary, and live regression coverage

Goal: Prove the corrected filters succeed against fixtures and live STRATZ while invalid values fail at the public MCP boundary.

Context:

- Existing live coverage exercises unfiltered player history but does not assert filtered request variables or error-code classification.
- The live test can discover a current numeric `gameVersionId` while selecting match seeds; local fixtures remain authoritative for exact role/position/lane mapping and error cases.

Files:

- Modify: `internal/domain/playermatch/service_test.go` — add table-driven valid mappings for all three roles and representative position/lane/patch combinations; assert native request variables and cursor binding.
- Modify: `internal/domain/leaguelive/service_test.go` — update patch fixtures to numeric game-version IDs and assert integer request variables if the shared patch fix is included.
- Modify: `internal/contracts/contracts_test.go` — validate the generated input schema's accepted/rejected filter values.
- Modify: `internal/mcp/player_match_test.go` and/or `internal/mcp/server_test.go` — call the public tool with invalid role/patch values and assert `isError` plus `error.code == INVALID_ARGUMENT`.
- Modify: `integration/live_test.go` — discover a numeric patch ID in the seed query; call player history with valid `role`, `position`, `lane`, and patch filters; add an error-call helper for the issue's invalid role examples and invalid patch, asserting `INVALID_ARGUMENT` and no `UPSTREAM_PROTOCOL_ERROR`.

Steps:

- [x] Use fixture responses with non-empty match rows for valid filter requests and assert the request variables rather than only asserting a non-error response.
- [x] Cover `CORE -> 0`, `LIGHT_SUPPORT -> 1`, `HARD_SUPPORT -> 2`, each concrete position enum, and all five lane IDs through table-driven tests.
- [x] Assert the public adapter rejects `role: "POSITION_1"`, `role: "1"`, unsupported position/lane values, and dotted/non-numeric patch IDs with the stable error code.
- [x] Extend live seed discovery only enough to obtain a current numeric game-version ID; do not require a particular role/position/lane distribution, and treat an empty filtered page as valid if the request itself succeeds.
- [x] Keep live operation coverage and existing non-zero semantic assertions intact; ensure filtered calls are recorded under the existing player-history operation names.

Verification:

- `go test ./internal/domain/playermatch ./internal/domain/leaguelive ./internal/contracts ./internal/mcp`
- `make generate`
- `make check-generated`
- `make check`
- `make test-live`
- `make build && CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp && CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp`

Completion criteria:

- Fixture tests prove valid filters produce integer/enum request variables and invalid inputs never invoke the executor.
- MCP and live tests classify unsupported values as `INVALID_ARGUMENT`, not `UPSTREAM_PROTOCOL_ERROR`.
- Live filtered requests reach current STRATZ successfully without requiring unstable result distributions.
- All generated artifacts and repository gates are clean.

## Cross-Task Verification

- `make generate`
- `make check-generated`
- `make check`
- `make test-live`
- `make build && CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp && CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp`

## Risks and Mitigations

- **Risk:** Issue #31 and issue #32 both change the draft-v2 contract version.
  **Mitigation:** Treat issue #31's planned `draft.2` migration as the shared boundary; never reset or duplicate the version. If the changes ship separately, make one monotonic version decision before generation.
- **Risk:** A client sends a human-readable patch such as `7.39`, while STRATZ requires a numeric game-version ID.
  **Mitigation:** Document numeric-ID semantics and reject dotted values locally rather than forwarding a request that STRATZ will reject.
- **Risk:** A future role/position/lane token is added upstream.
  **Mitigation:** Keep the public enum closed and return `INVALID_ARGUMENT` until an explicit mapping and fixture are added.
- **Risk:** The MCP adapter and direct domain entry points drift in validation behavior.
  **Mitigation:** Use one shared domain parser and test both the direct service and MCP boundary; keep hero resolution as the only adapter-specific step.
- **Risk:** Filtered live calls return no rows or upstream data distributions change.
  **Mitigation:** Assert successful request/error classification and captured request semantics; use deterministic fixtures for complete value coverage.
- **Risk:** Updating the shared patch contract affects league history.
  **Mitigation:** Update the league request schema/mapping/tests in the same change, or explicitly keep the league branch unchanged rather than leaving a second known string-to-Int failure.

## Open Questions

- None for implementation. The plan chooses symbolic role/position/lane filters and numeric game-version IDs; product changes to human-readable patch resolution would be a separate contract decision.
