# Expose Semantic Match Position and Player Metadata

## Overview

Expose STRATZ player metadata in exact-match players and player-history player rows without conflating Dota position with the player slot. The current public `position` field is incorrectly populated from upstream `playerSlot`; retain that slot under public `player_slot`, and populate semantic `position` from STRATZ's separate upstream `position` field. Add `role`, `role_basic`, `lane`, and nullable `party_id` alongside it.

## Source Spec

- Spec: [GitHub issue #31](https://github.com/aneviaro/stratz-mcp/issues/31)
- Status: Open; implementation plan assumes the contract decision below
- Last reviewed: 2026-10-18

## Repository Context

- `docs/tool-contracts.json` — canonical public JSON Schema registry and shared `matchPlayer` definition.
- `docs/tool-contracts.md` and `docs/architecture-spec.md` — normative contract/version and migration documentation.
- `internal/contractgen/` and `internal/contracts/` — generated public Go types, schemas, examples, protocol fixtures, and reference documentation.
- `internal/graphql/schema/bootstrap.graphql` — committed development schema used to validate and generate curated operations.
- `internal/graphql/operations/match.graphql` — shared `MatchPlayerFields` fragment used by exact matches and player-history detail rows.
- `internal/domain/playermatch/types.go` — normalized and tolerant upstream player-row types.
- `internal/domain/playermatch/mapping.go` — maps exact and player-history upstream rows into the normalized shape.
- `internal/domain/playermatch/service.go` — routes exact and player-history modes; both paths already share the match-player mapping.
- `internal/domain/playermatch/service_test.go`, `internal/mcp/player_match_test.go`, and `integration/live_test.go` — domain, adapter/schema, and live semantic coverage.

## Implementation Constraints

- Treat `docs/tool-contracts.json`, `internal/graphql/operations/*.graphql`, and `internal/graphql/schema/bootstrap.graphql` as canonical; regenerate all derived artifacts with `make generate`.
- Do not hand-edit `internal/graphql/generated/**`, `internal/contracts/generated/**`, `internal/contracts/zz_generated.contracts.go`, or `docs/generated-tool-contracts.md`.
- Keep upstream `playerSlot` normalized and bounded to `0..9` as the public `player_slot` field; `player_slot` is a project-owned output name, not an upstream STRATZ field. Never derive semantic position from slot number.
- Preserve upstream `null` as JSON `null`. Preserve an upstream `UNKNOWN` enum as `UNKNOWN`; map an unrecognized non-null enum token to `UNKNOWN` rather than fabricating a specific role, lane, or position.
- Keep all new normalized fields non-`omitempty` so contract-required nullable properties are emitted explicitly as `null`.
- Keep `livePlayer` unchanged; issue #31 acceptance scope is exact-match and player-history player rows, not live-match rows.
- Keep MCP handlers as adapters and preserve JSON-RPC-only stdout, centralized redaction, the fixed production STRATZ endpoint, and bounded upstream execution.

## Assumptions

- This is an intentional breaking semantic change within the draft v2 contract: bump `contractVersion` from `2.0.0-draft.1` to `2.0.0-draft.2` and document the migration. Existing clients that read integer `position` must read `player_slot` instead.
- `matchPlayer.position` allows `POSITION_1` through `POSITION_5`, `UNKNOWN`, or `null`. `role` and `role_basic` allow `CORE`, `LIGHT_SUPPORT`, `HARD_SUPPORT`, `UNKNOWN`, or `null`. `lane` allows `SAFE_LANE`, `MID_LANE`, `OFF_LANE`, `JUNGLE`, `ROAMING`, `UNKNOWN`, or `null`.
- `party_id` is a nullable non-negative integer and remains `null` when STRATZ does not provide it.
- New fields are required properties of the normalized `matchPlayer` object, with nullable values where upstream data is nullable, matching the existing explicit-null contract style.
- A live run may not contain every position/role/lane category; deterministic fixtures will cover carry, mid, offlane, and support semantics, while live tests verify compatibility and observed non-null values.

## Non-goals

- Adding these fields to `livePlayer` or changing the meaning of live-match `position`.
- Adding or changing public player-history filters, role filter translation, pagination, caching, or request budgets.
- Inferring missing semantic metadata from `player_slot`, team, hero, or lane heuristics.
- Committing fetched restricted STRATZ schema/constants artifacts.
- Changing unrelated hero-stat position enums or other match/player output shapes.

## Task Summary

1. Freeze the public field semantics, version bump, and migration guidance.
2. Select the upstream fields in the canonical schema and shared GraphQL fragment.
3. Decode and map metadata without losing null/unknown distinctions.
4. Add semantic contract, domain, adapter, and live coverage, then run all acceptance gates.

## Implementation Tasks

### Task 1: Freeze the match-player contract and migration

Goal: Make the new field names, enum values, nullability, and breaking-version decision authoritative before changing normalized output.

Context:

- The current public `matchPlayer.position` incorrectly contains the normalized upstream `playerSlot` (`0..9`), while the requested semantic position must be decoded from STRATZ's separate upstream `position` enum.
- `internal/contractgen/generator.go` validates the exact contract version and emits all public artifacts from the registry.

Files:

- Modify: `docs/tool-contracts.json` — bump to `2.0.0-draft.2`; replace integer `position` with nullable semantic enum; add `player_slot`, `role`, `role_basic`, `lane`, and `party_id` to `$defs.matchPlayer` and its required list.
- Modify: `internal/contractgen/generator.go` — update the expected contract version to `2.0.0-draft.2`.
- Modify: `docs/tool-contracts.md` — document exact/history player-row semantics, explicit-null behavior, live-player non-goal, and the v2.0.0-draft.1-to-draft.2 migration.
- Modify: `docs/architecture-spec.md` — update the normative current contract-version reference and state that the semantic `position` change is versioned, not silent.
- Modify: `internal/contracts/contracts_test.go` — update representative generated match-player fixtures and add schema checks for the new required fields, nullable values, `UNKNOWN`, and rejection of the old integer `position` shape.
- Generated: `internal/contracts/zz_generated.contracts.go`, `internal/contracts/generated/**`, and `docs/generated-tool-contracts.md` — regenerate; do not edit directly.

Steps:

- [x] Define the public `matchPlayer.player_slot` as a required integer in `0..9`, explicitly documenting that it is derived from upstream `playerSlot`.
- [x] Define `position`, `role`, `role_basic`, and `lane` as required nullable enum properties with the allowed values in the assumptions above; define nullable non-negative `party_id`.
- [x] Bump the canonical registry and generator expectation together, and document that clients reading the old integer `position` must migrate to the public `player_slot` field.
- [x] Update contract fixtures/tests so a player row with representative values and a player row with explicit null metadata validate at the generated-schema boundary.
- [x] Run generation and inspect the diff for only the intended contract/version artifacts.

Verification:

- `make generate`
- `make check-generated`
- `go test ./internal/contractgen ./internal/contracts`

Completion criteria:

- Generated `contracts.MatchPlayer` exposes the new fields with the intended Go types and JSON names.
- The generated exact-match and player-history schemas require the new properties, allow explicit nulls/`UNKNOWN`, and reject an integer semantic `position`.
- Documentation states the versioned migration and does not imply that `livePlayer.position` has changed.

### Task 2: Select upstream player metadata in canonical GraphQL

Goal: Ensure both exact-match and player-history player rows request the upstream position, role, lane, and party fields.

Context:

- `MatchPlayerFields` is reused by `StratzGetMatch*`, batch exact operations, and `StratzListPlayerMatchesWithPlayers`.
- The authenticated STRATZ match schema exposes nullable `position: MatchPlayerPositionType`, `role: MatchPlayerRoleType`, `roleBasic: MatchPlayerRoleType`, `lane: MatchLaneType`, and `partyId`.

Files:

- Modify: `internal/graphql/schema/bootstrap.graphql` — add `MatchLaneType` and `MatchPlayerRoleType` enums and the nullable fields to `MatchPlayerType`.
- Modify: `internal/graphql/operations/match.graphql` — add `position`, `role`, `roleBasic`, `lane`, and `partyId` to `MatchPlayerFields`, preserving the existing `playerSlot` selection.
- Generated: `internal/graphql/generated/operations.go` and `internal/graphql/generated/operations.json` — regenerate from the canonical schema/operations.

Steps:

- [x] Add the upstream enum values, including `UNKNOWN`, without making nullable fields non-null in the bootstrap schema.
- [x] Select all five new upstream fields in the shared fragment so exact and player-history detail use the same source and cannot drift.
- [x] Keep `playerSlot` selected as the source for the separately normalized `player_slot` field.
- [x] Regenerate GraphQL artifacts and confirm generated response fields are nullable enum pointers plus a nullable integer party ID.
- [x] Verify no live-match operation or unrelated GraphQL field selection changes.

Verification:

- `make generate`
- `make check-generated`
- `go test ./internal/graphql/...`
- Inspect generated `MatchPlayerFields` and the operation text for all exact and player-history operation variants.

Completion criteria:

- Every curated exact/player-history operation that returns player rows selects all requested upstream metadata fields.
- The generated GraphQL package compiles against the committed bootstrap schema and retains nullable enum types.
- No generated restricted schema or fetched upstream artifact is added to the repository.

### Task 3: Map metadata without deriving or fabricating semantics

Goal: Serialize normalized `player_slot`, semantic metadata, and nullable party IDs consistently from both exact and player-history rows.

Context:

- `mapMatch` and `findMatchPlayer` currently normalize upstream `playerSlot` into this repository's domain `MatchPlayer.Position`; this domain field must instead receive STRATZ's separate semantic `position`, while the slot becomes `PlayerSlot` serialized as public `player_slot`.
- `upstreamMatchPlayer` is this repository's local curated-JSON decoder, not an upstream Go type. Its `Position` field represents the upstream GraphQL/JSON `position`; generated GraphQL code represents that same field as `MatchPlayerPositionType`.

Files:

- Modify: `internal/domain/playermatch/types.go` — add `PlayerSlot int64` plus nullable semantic `Position`, `Role`, `RoleBasic`, `Lane`, and `PartyID` fields to the domain `MatchPlayer`; add corresponding upstream JSON fields to the local decoder structs.
- Modify: `internal/domain/playermatch/mapping.go` — map the normalized slot separately and normalize upstream enum tokens with nil-preserving, bounded helpers in both exact and history paths.
- Test: `internal/domain/playermatch/service_test.go` — extend fixtures for non-zero metadata, nulls, `UNKNOWN`, and the distinction between slot and semantic position.

Steps:

- [x] Map upstream `playerSlot` to `MatchPlayer.PlayerSlot` serialized as `player_slot`, and map upstream semantic `position` to nullable `MatchPlayer.Position` serialized as `position`; add the other public JSON fields without `omitempty`.
- [x] Decode `position`, `role`, `roleBasic`, `lane`, and `partyId` using their upstream JSON names and nullable representations.
- [x] Add bounded enum normalization: nil stays nil, recognized values pass through, `UNKNOWN` stays `UNKNOWN`, and unknown non-null tokens become `UNKNOWN`.
- [x] Update both `mapMatch` and `findMatchPlayer` to use the same helper and never infer semantic position from the normalized slot.
- [x] Preserve existing team, KDA, win, IMP, hero-name, and slot normalization behavior.

Verification:

- `go test ./internal/domain/playermatch`
- Marshal representative `Match` and `PlayerMatchSummary.Player` values and inspect that all new keys are present, with nulls preserved.

Completion criteria:

- Exact match rows and player-history detail rows expose identical metadata semantics.
- A slot such as normalized `0` or `5` does not determine `POSITION_1` or another semantic position; only STRATZ's upstream `position` value may populate public `position`, and upstream null remains null.
- Missing/null upstream values serialize as explicit null and no fabricated role, lane, party, or position is emitted.

### Task 4: Add semantic boundary and live coverage

Goal: Prove the public wire contract carries meaningful carry, mid, offlane, and support examples and remains compatible with current STRATZ data.

Context:

- Schema-only tests would miss a mapping regression because zero values can still satisfy integer-shaped fixtures.
- The live suite already exercises exact `players` detail and player-history `detail_level: "players"`; it should validate field presence without requiring unstable live category distributions.

Files:

- Modify: `internal/domain/playermatch/service_test.go` — add deterministic exact and player-history fixtures for carry, mid, offlane, and support rows plus null/unknown cases.
- Modify: `internal/mcp/player_match_test.go` — pass a representative populated player row through the MCP success/schema validation boundary.
- Modify: `internal/contracts/contracts_test.go` — add semantic output cases if needed to cover enum/null combinations after generation.
- Modify: `integration/live_test.go` — assert exact and player-history player rows contain `player_slot`, nullable semantic metadata, and valid observed enum values.

Steps:

- [x] Use non-zero fixtures that distinguish `player_slot` from semantic position and cover `POSITION_1`, mid, offlane, and both core/support role examples.
- [x] Assert `role_basic` and `lane` are mapped independently of `role`, and assert `party_id` survives when present and remains null when absent.
- [x] Assert exact and player-history rows include the new keys and preserve explicit null/`UNKNOWN`; do not assert that a live match contains every category.
- [x] Keep existing assertions for non-zero KDA, team, win state, and detail-level behavior so the new fields cannot mask regressions.
- [x] Run the complete generation, unit, MCP interoperability, and live acceptance gates.

Verification:

- `go test ./internal/domain/playermatch ./internal/contracts ./internal/mcp`
- `make generate`
- `make check-generated`
- `make check`
- `make test-live`
- `make build && CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp && CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp`

Completion criteria:

- Local semantic tests prove carry, mid, offlane, and support rows retain the intended enums and non-zero slot/party values.
- Exact and player-history MCP responses validate against the regenerated closed schema with no dropped or fabricated metadata.
- Live curated operations return compatible player metadata when STRATZ provides it, and all required project checks pass.

## Cross-Task Verification

- `make generate`
- `make check-generated`
- `make check`
- `make test-live`
- `make build && CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp && CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp`

## Risks and Mitigations

- **Risk:** Changing `position` from integer to enum breaks existing v2 clients.
  **Mitigation:** Bump to `2.0.0-draft.2`, document the exact `position` → `player_slot` migration, and reject the old shape through the generated schema rather than silently accepting ambiguous data.
- **Risk:** STRATZ returns null, `UNKNOWN`, or a future enum token.
  **Mitigation:** Use nullable fields, preserve `UNKNOWN`, and map only unrecognized non-null tokens to the explicit `UNKNOWN` sentinel; never infer from slot or hero.
- **Risk:** Required contract properties cause old hand-built fixtures to fail.
  **Mitigation:** Regenerate examples and update all semantic fixtures to include every field, using explicit null where upstream data is unavailable.
- **Risk:** Exact and player-history selections drift apart.
  **Mitigation:** Add fields only to the shared `MatchPlayerFields` fragment and assert both operation families in tests.
- **Risk:** Live data does not cover all requested role/position categories on every run.
  **Mitigation:** Keep category coverage deterministic in local fixtures; make live assertions validate presence, nullability, and allowed values rather than unstable distributions.

## Open Questions

- None. The plan intentionally chooses a versioned breaking rename within draft v2 and leaves live-player metadata out of scope.
