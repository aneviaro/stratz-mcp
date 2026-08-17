---
Created: 2026-06-23
Purpose: Provide shared project-local guidance for AI-assisted changes.
Status: Current
---

Be concise, decisive, and implementation-first.

When uncertain, make the most reasonable assumption and proceed. Ask only if blocked.

Use this loop:
1. Inspect the minimum necessary context.
2. Choose one path.
3. Make the change.
4. Run the smallest relevant test.
5. Report result.

Avoid:
- brainstorming
- hedging
- repeated summaries
- explaining obvious steps
- offering multiple options unless asked

Every response must end with either:
- DONE
- NEXT: <one concrete action>

# Project guidance

- Treat `docs/tool-contracts.json`, `internal/graphql/operations/*.graphql`, `internal/graphql/schema/bootstrap.graphql`, and `workflows/workflows.json` as canonical sources. Do not edit generated outputs directly.
- Run `make generate` after canonical-source changes, `make check-generated` to detect stale artifacts, and both `make check` and `make test-live` before handoff.
- Use conventional commit messages, such as `feat: add optional player rows to match list`.
- Never commit tokens, `.env`, cache databases, introspection, fetched STRATZ schemas/constants, or `.stratz-restricted`.
- Preserve JSON-RPC-only stdout and centralized secret redaction.
- Keep the production STRATZ endpoint fixed; inject executors only in tests.
- Keep MCP handlers as adapters. Put normalization, pagination, validation, and upstream mapping in domain packages.
- Run `go test -race ./...` for concurrency or security changes, both client-profile smoke tests for MCP changes, and local container checks for Docker changes.

## Architecture and ownership

- `internal/app` is the composition root; `internal/cli` owns commands; `internal/mcp` is the transport adapter.
- `internal/domain/{playermatch,heroconstants,leaguelive}` owns domain validation and mapping. Shared batching and cursors live under `internal/domain`.
- `internal/stratz` owns bounded HTTP execution and stable upstream errors. `internal/graphql/policy` owns raw-query validation and demand control.
- `internal/cache` owns SQLite migrations, classifications, compression, and degraded-mode behavior. `internal/schema` owns restricted schema pull/generation.
- Generated contract, GraphQL, prompt, skill, fixture, and reference files must be changed through their generators.

## Upstream schema changes

- Treat a compiling GraphQL operation as necessary but insufficient. Verify that normalized output still carries the promised meaning.
- Do not silently ignore public filters or return requested filters in provenance unless they were applied upstream or locally.
- If STRATZ cannot represent a public filter or detail field honestly, return an explicit `INVALID_ARGUMENT`, warning, or nullable/omitted value according to the contract. Never fabricate zeros or empty collections as if data was observed.
- Keep compatibility decoding bounded and intentional when STRATZ changes scalar representations, such as numeric IDs becoming enums or kill totals becoming event arrays.
- Update the bootstrap schema, canonical operations, tolerant domain decoders, mappings, and semantic tests together.

## Known STRATZ mappings

- Live player KDA fields are `numKills`, `numDeaths`, and `numAssists`; alias them to the normalized names. Missing selections otherwise decode to misleading zero values.
- Current match detail is exposed through `playbackData`. Standard detail can derive objectives from building, Roshan, and tower-death events. Full detail can additionally derive timeline entries from rune and ward events.
- `MatchType.gameMode` may return enum names. `ALL_PICK_RANKED` maps to Dota game mode ID `22`.
- Dire player slots `128..132` normalize to positions `5..9`.

## Hero statistics

- Current bounded time-series operations are `winDay`, `winWeek`, and `winMonth`, grouped by `TIME`. Aggregate only rows inside the effective half-open range `[from, to)`.
- Time-grouped rows use Unix-second period values and may omit `heroId` even when one `heroIds` value was supplied. A decoded hero ID of zero is acceptable only when the request was scoped to exactly one known hero.
- Rank filters map to STRATZ `RankBracket` values. Role filters map to one or more `MatchPlayerPositionType` values.
- Pick and ban rates are not derivable from the win aggregate alone. Return them as unavailable with a warning rather than inventing denominators.
- Patch, lane, matchup, and synergy dimensions must be rejected explicitly until a correct bounded mapping exists.

## Verification

- Tests must assert semantics, not only GraphQL selections, output schemas, operation names, or non-error responses. Include representative non-zero counters and non-empty detail events.
- Run `make test-live` for any curated-operation, upstream-schema, decoder, enum, filter, or mapping change. The live suite is part of correctness.
- Use STRATZ MCP tools for live validation when requested. Raw GraphQL through the MCP requires a pulled local schema bundle and may fail before reaching STRATZ; prefer curated MCP tools when they cover the behavior.
- Temporary diagnostics may expose only bounded, non-secret upstream metadata and must be removed once the mismatch is understood.

## Libraries and commands

- Core libraries: official MCP Go SDK, genqlient, gqlparser, strict YAML v3, modernc SQLite, Zstandard, and `log/slog`.
- Run `CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp` and the equivalent `claude` profile after MCP changes.
- Public-source checks include `make public-readiness`, `make check-restricted`, and `make notices`.

## Debugging

- Start with `stratz-mcp doctor`; use `cache stats`, `schema pull`, and debug-level stderr logs for the relevant subsystem.
- Raw GraphQL is unavailable without a valid local schema bundle.
- Cache failures intentionally degrade to uncached execution. Preserve the reported reason before clearing data.
- If Go reports `GOPROXY list is not empty, but contains no entries`, build with `GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org`.
