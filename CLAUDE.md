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
