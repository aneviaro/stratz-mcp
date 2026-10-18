# Expose IMP in Normalized Match Player Outputs

## Overview

Expose STRATZ's nullable IMP score in the normalized player rows returned by `stratz_query_matches` exact-match results and player-history results with `detail_level: "players"`. The upstream GraphQL selection and domain mapping already carry positive, negative, and null IMP values; the loss occurs at the normalized JSON serialization and public contract boundary.

## Source Spec

- Spec: [GitHub issue #30](https://github.com/aneviaro/stratz-mcp/issues/30)
- Status: Approved for implementation planning
- Last reviewed: 2026-10-18

## Repository Context

- `docs/tool-contracts.json` — canonical public JSON Schema registry; `$defs.matchPlayer` currently omits `imp`.
- `internal/contractgen/` — generates public Go contract types, embedded schemas, examples, protocol fixtures, and generated contract documentation.
- `internal/domain/playermatch/types.go` — normalized internal `MatchPlayer`; its `Imp` pointer is currently tagged `json:"-"`.
- `internal/domain/playermatch/mapping.go` — already maps upstream `imp` into both exact-match and player-history player rows.
- `internal/graphql/operations/match.graphql` — already selects `imp` through `MatchPlayerFields` for exact and player-history operations.
- `internal/domain/playermatch/service_test.go` — existing fixtures cover positive and null upstream IMP values and internal mapping.
- `internal/contracts/contracts_test.go` — generated-schema semantic tests and player-history output helper.
- `Makefile` — `generate`, `check-generated`, `check`, and `test-live` acceptance commands.

## Implementation Constraints

- Edit canonical sources only; do not hand-edit `internal/contracts/generated/**`, `internal/contracts/zz_generated.contracts.go`, or `docs/generated-tool-contracts.md`.
- Keep IMP as `*float64` so upstream `null` serializes as JSON `null`; do not convert absent values to zero or omit the field.
- Keep `imp` in the shared `matchPlayer` shape used by exact matches and player-history `player` rows. Do not expand `livePlayer` without an upstream field and an explicit contract requirement.
- No GraphQL operation or bootstrap schema change is expected because `imp: Float` is already selected and represented.
- Preserve the existing JSON-RPC boundary, centralized redaction, and MCP adapter/domain ownership.

## Assumptions

- `imp` is a required serialized property whose value is nullable, matching the existing non-`omitempty` behavior of normalized player fields and the issue's requirement to preserve `null`.
- The live-match player shape remains unchanged because issue #30 scopes the fix to exact matches and player history.
- Local semantic fixtures are authoritative for positive, negative, and null values; live tests validate compatibility with current STRATZ data but do not require a live match to produce all three cases.

## Non-goals

- Changing the upstream GraphQL operation, STRATZ schema, IMP calculation, or numeric interpretation.
- Adding IMP to profile players, live players, or unrelated normalized output shapes.
- Refactoring the existing match/player mapping or pagination behavior.

## Task Summary

1. Add nullable IMP to the canonical `matchPlayer` contract and regenerate artifacts.
2. Serialize IMP from normalized match players and add semantic exact/history coverage.

## Implementation Tasks

### Task 1: Add nullable IMP to the canonical match-player contract

Goal: Make `imp` a documented nullable numeric field in every public match-player row and regenerate all derived contract artifacts.

Context:
- `docs/tool-contracts.json` is canonical and currently defines `matchPlayer` without IMP.
- The generated `contracts.MatchPlayer` type and output schema must agree with the normalized JSON emitted by the domain layer.
- Generated outputs must be recreated by the repository generator, not edited directly.

Files:

- Modify: `docs/tool-contracts.json` — add `imp` to `$defs.matchPlayer.properties` as `number | null` and add it to that definition's required fields.
- Generated: `internal/contracts/zz_generated.contracts.go` — generated `contracts.MatchPlayer.Imp *float64`.
- Generated: `internal/contracts/generated/schemas/stratz_query_matches.output.json` — exact and player-history output schemas include nullable IMP.
- Generated: `internal/contracts/generated/examples/**`, `internal/contracts/generated/protocol/**`, `internal/contracts/generated/manifest.json`, and `docs/generated-tool-contracts.md` — update only if generation changes them deterministically.

Steps:

- [ ] Update only the canonical `matchPlayer` definition; use a nullable numeric schema and require the `imp` property so missing upstream values are represented as explicit `null`.
- [ ] Run `make generate` from the repository root.
- [ ] Review generated diffs to confirm the change is limited to the expected match-output contract artifacts and that no generated file was hand-edited.
- [ ] Confirm the generated Go type exposes `Imp *float64` with JSON key `imp` and the generated output schema accepts both numbers and `null`.

Verification:

- `make generate`
- `make check-generated`
- `go test ./internal/contractgen ./internal/contracts`

Completion criteria:

- The canonical registry, generated Go types, embedded schemas, examples/fixtures, and generated contract reference agree on nullable `matchPlayer.imp`.
- Exact-match and player-history output branches validate a player row containing positive, negative, or null IMP.

### Task 2: Serialize normalized IMP and add semantic regression tests

Goal: Ensure exact matches and player-history player rows emit the mapped IMP value, including negative values and explicit `null`.

Context:
- `internal/domain/playermatch/mapping.go` already assigns `upstreamMatchPlayer.IMP` in `mapMatch` and `findMatchPlayer`; only the JSON boundary currently discards it.
- `MatchPlayer.Imp` must remain a pointer without `omitempty` so `nil` becomes JSON `null`.
- Existing tests inspect internal pointers, so they do not catch the current omission from public JSON.

Files:

- Modify: `internal/domain/playermatch/types.go` — change `MatchPlayer.Imp` from `json:"-"` to `json:"imp"`.
- Modify: `internal/domain/playermatch/service_test.go` — add or extend exact and player-history semantic tests that marshal normalized results and assert positive, negative, and null JSON values.
- Modify: `internal/contracts/contracts_test.go` — update generated player-history fixtures/helpers to include required `imp`, and validate representative numeric/null values if needed to prove the schema boundary.
- Do not modify: `internal/domain/playermatch/mapping.go` or `internal/graphql/operations/match.graphql` unless a test disproves the current mapping/selection behavior.

Steps:

- [ ] Change the normalized `MatchPlayer.Imp` JSON tag to `json:"imp"` while retaining type `*float64` and no `omitempty`.
- [ ] Extend exact-match fixture coverage with a negative IMP and assert the marshaled `Match.players[].imp` is the same negative number.
- [ ] Extend player-history `detail_level: "players"` coverage with a positive IMP and an upstream `imp: null`; assert the serialized `player.imp` values are the number and JSON `null`, never zero and never omitted.
- [ ] Exercise the public contract validation path for representative positive, negative, and null player rows so schema acceptance and wire serialization are tested together.
- [ ] Keep existing internal pointer assertions and query-selection assertions to guard upstream decoding and operation coverage.

Verification:

- `go test ./internal/domain/playermatch ./internal/contracts ./internal/mcp`
- Inspect marshaled test output or decoded JSON maps to verify `imp` is present with the expected number or `nil`.

Completion criteria:

- Exact normalized matches expose `players[].imp`.
- Player-history rows with player detail expose `player.imp`.
- Positive and negative upstream values round-trip unchanged, while upstream `null` is emitted as JSON `null` and never fabricated as `0`.
- No unrelated player shape or mapping behavior changes.

## Cross-Task Verification

- `make generate`
- `make check-generated`
- `make check`
- `make test-live`
- If live STRATZ data lacks a stable negative or null sample, record that local semantic fixtures cover those cases and retain live verification for operation compatibility and non-null field decoding.

## Risks and Mitigations

- **Risk:** Adding a required schema property could reject existing hand-built test payloads that omit `imp`.
  **Mitigation:** Update canonical examples and test helpers to emit `imp: null`; keep the Go field non-omitempty so real normalized output always satisfies the contract.
- **Risk:** A future edit adds `omitempty` and silently reintroduces omission for null IMP.
  **Mitigation:** Assert key presence and JSON `null` in semantic serialization tests.
- **Risk:** Live STRATZ data changes IMP availability or representation.
  **Mitigation:** Keep decoder compatibility bounded to the existing nullable `Float` representation and use deterministic local fixtures for all sign/null cases.

## Open Questions

- None.
