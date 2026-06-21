---
Created: 2026-06-21
Purpose: Provide project-local guidance for AI-assisted changes.
Status: Current
---

# Project guidance

- Treat `docs/tool-contracts.json`, `internal/graphql/operations/*.graphql`, and `workflows/workflows.json` as canonical sources. Do not edit generated outputs directly.
- Run `go generate ./...` and `make check-generated` after canonical-source changes.
- Never commit tokens, `.env`, cache databases, introspection, fetched STRATZ schemas/constants, or `.stratz-restricted`.
- Preserve JSON-RPC-only stdout and centralized secret redaction.
- Keep the production STRATZ endpoint fixed; inject executors only in tests.
- Keep MCP handlers as adapters. Put normalization, pagination, and upstream mapping in domain packages.
- Run `make check` before handoff. Also run `go test -race ./...` for concurrency/security changes, both client-profile smoke tests for MCP changes, and container/release-policy checks for packaging changes.

## Architecture and ownership

- `internal/app` is the composition root; `internal/cli` owns commands; `internal/mcp` is the transport adapter.
- `internal/domain/{playermatch,heroconstants,leaguelive}` owns domain validation and mapping. Shared batching and cursors live under `internal/domain`.
- `internal/stratz` owns bounded HTTP execution and stable upstream errors. `internal/graphql/policy` owns raw-query validation and demand control.
- `internal/cache` owns SQLite migrations, classifications, compression, and degraded-mode behavior. `internal/schema` owns restricted schema pull/generation.
- Canonical generation inputs are `docs/tool-contracts.json`, `internal/graphql/operations/*.graphql`, `internal/graphql/schema/bootstrap.graphql`, and `workflows/workflows.json`.
- Generated contract, GraphQL, prompt, skill, fixture, and reference files must be changed through their generators.

## Libraries and commands

- Core libraries: official MCP Go SDK, genqlient, gqlparser, strict YAML v3, modernc SQLite, Zstandard, and `log/slog`.
- Run `make generate` after canonical-source changes, `make check-generated` to detect stale artifacts, and `make check` for the standard gate.
- Run `CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp` and the equivalent `claude` profile after MCP changes.
- Packaging checks include `make package`, `make check-policies`, `make check-restricted`, and `make notices`.

## Debugging

- Start with `stratz-mcp doctor`; use `cache stats`, `schema pull`, and debug-level stderr logs for the relevant subsystem.
- Raw GraphQL is unavailable without a valid local schema bundle.
- Cache failures intentionally degrade to uncached execution. Preserve the reported reason before clearing data.
