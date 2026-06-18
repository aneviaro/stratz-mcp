---
Created: 2026-06-18
Purpose: Define the dependency-ordered, testable implementation plan for STRATZ MCP v1.
Status: Ready for implementation; public release remains blocked on STRATZ API-use clearance
---

# STRATZ MCP v1 Implementation Plan

## Summary

Implement the architecture in dependency-ordered milestones. Every step ends with an automated or evidence-based completion gate. Local development may proceed before STRATZ permission clearance, but publishing remains blocked.

## Locked interfaces and dependencies

- Preserve all 15 tools and schemas from `docs/tool-contracts.json`.
- Expose the specified resources, five prompts, CLI commands, configuration precedence, error envelope, and cursor format unchanged.
- Use:
  - Official MCP Go SDK `v1.6.1`, which supports MCP `2025-11-25`, `outputSchema`, `structuredContent`, and `isError`.
  - `genqlient` for curated operations.
  - `gqlparser/v2` for raw-query parsing and validation.
  - `modernc.org/sqlite` for CGO-free caching.
  - `log/slog`, strict YAML v3 decoding, and Zstandard compression.
- Keep the production endpoint fixed; permit endpoint injection only through internal test constructors.

## Implementation steps

### 1. Close discovery and release-gate evidence

- Complete conservative fixtures or safe live probes for private profiles, partial GraphQL results, timeout, oversized response, and invalid credentials.
- Record STRATZ permission decisions for API wrapping, caching, schema/constants redistribution, attribution, and branding.

**Verification:** Every discovery-matrix row has dated evidence or an explicitly documented mock-based policy. A release-gate test fails unless all required clearance fields are approved.

### 2. Establish project structure and dependency baseline

- Add the production command, recommended internal packages, pinned dependencies, generation entrypoints, and build-version injection.
- Retain existing discovery commands as development utilities.

**Verification:** `go mod verify`, `go vet ./...`, and `go test ./...` pass on Go 1.25 and 1.26. A minimal cross-compile succeeds for every native target.

### 3. Generate the public contract

- Build a deterministic generator that validates and dereferences `tool-contracts.json`, then emits Go contract types, embedded schemas, validators, protocol fixtures, and reference documentation.
- Reject unsupported schema constructs instead of weakening validation.

**Verification:** All 15 tools have Draft 2020-12 input/output schemas. Generated examples validate. `go generate ./...` produces no diff on a clean tree.

### 4. Implement configuration, credentials, and safe logging

- Add strict CLI/environment/YAML precedence, explicit dotenv loading, mutually exclusive secret sources, bounded token-file parsing, permission diagnostics, and centralized redaction.
- Implement `version` and command help behavior.

**Verification:** Table-driven tests cover precedence, unknown YAML keys, conflicting sources, symlinks, multiline/NUL/oversized tokens, absent credentials, and redaction of every sensitive header.

### 5. Build the bounded STRATZ HTTP client

- Implement fixed-endpoint POST requests, required headers, gzip streaming, redirect refusal, decompressed-size limits, cancellation, retries, WAF detection, rate-window parsing, and stable error mapping.
- Enforce the five-round-trip budget per MCP call.

**Verification:** Mock-server tests cover every HTTP/network mapping, gzip bombs, malformed JSON, redirects, partial results, WAF HTML, retry timing, cancellation, and request-budget exhaustion without leaking response bodies or sensitive headers.

### 6. Implement MCP lifecycle and wire behavior

- Register static tools, resources, and prompts over stdio using the official SDK.
- Create one result encoder that emits authoritative `structuredContent`, its exact compact JSON text mirror, correct `outputSchema`, and execution-error `isError`.
- Add `doctor` and `stratz_server_info`.

**Verification:** Protocol tests negotiate `2025-11-25`, reject pre-initialization calls, keep stdout JSON-RPC-only, distinguish protocol and tool errors, and byte-compare the text mirror against compact `structuredContent`.

### 7. Implement raw GraphQL policy and execution

- Parse and validate one query operation; expand fragments; enforce default-deny roots, introspection policy, variable limits, depth, aliases, field counts, list bounds, nested lists, complexity, request budget, and response size.
- Preserve bounded upstream `data`, `errors`, and `extensions`; mark partial raw responses.

**Verification:** Adversarial tests cover aliases, fragments, cycles, directives, denied/unknown roots, mutations, subscriptions, introspection, unbounded lists, variable-supplied limits, and every raw-query error code.

### 8. Implement schema lifecycle and resources

- Add authenticated deterministic `schema pull`, schema hashing, domain subset generation, local-only generated artifacts before redistribution clearance, and all required schema/constants resource handlers.

**Verification:** Fixture-based schema pulls are deterministic. Generated operations compile. Resource lists and reads match expected URIs. CI detects schema drift and accidentally committed restricted upstream artifacts.

### 9. Implement pagination and batch primitives

- Add canonical filter hashing, HKDF/HMAC cursor signing, token/tool/version binding, expiry classes, bounded scan continuation, deduplication, ordering reconstruction, cancellation, and atomic batch errors.

**Verification:** Tests cover tampering, wrong filters/tools/tokens, rotation, expiry, restart stability, duplicate inputs, first-failure cancellation, exact ordering, and the five-request ceiling for 25-item batches.

### 10. Implement SQLite caching

- Add migrations, WAL mode, busy timeout, token namespaces, cache classes, TTL/stale rules, asynchronous writes, Zstandard compression, LRU eviction, permissions, corruption fallback, and cache CLI commands.
- Disable raw caching until field classifications are approved.

**Verification:** Tests cover hit/miss/stale/fresh behavior, exclusions, namespace isolation, compression threshold, eviction, concurrent processes, lock/corruption fallback, permissions, symlink handling, statistics, and transactional clearing.

### 11. Implement player and match domains

- Add player identifier normalization, singular/list/batch player operations, match detail levels, normalized mappings, bounded minimum-duration scanning, and required `DATA_NOT_READY` context.

**Verification:** Contract fixtures cover account ID, SteamID64, URLs, private/missing players, list continuation, batch atomicity, parsed/unparsed matches, detail-level field inclusion, partial upstream failures, and raw bypass behavior.

### 12. Implement hero and constants domains

- Add cached hero lookup indexes, ambiguity handling, hero batches, constants retrieval, statistics bucket translation, denominator calculation, matchup/synergy mapping, and unsupported filter handling.

**Verification:** Tests cover ID/name/slug lookup, ambiguous suggestions, duplicate batches, each constants type, explicit `all`, effective date ranges, patch incompatibilities, rate calculations, and absent rank redistribution data.

### 13. Implement league and live-match domains

- Add league retrieval/listing/matches, derived status, bounded text scans, live native filters, bounded client-side filters, and truthful incomplete-scan cursors.

**Verification:** Tests cover native and client-side filters, five-page exhaustion, continuation, derived statuses, changing live data, sort modes, and confirmation that unsupported region filtering is absent.

### 14. Generate workflows, prompts, and portable skills

- Define one canonical workflow source and generate the five MCP prompts plus five `SKILL.md` packages with Codex and Claude adapters.
- Encode attribution, provenance, fact-versus-interpretation rules, and untrusted-content defenses.

**Verification:** Generation is reproducible. Skill validation passes. Injection fixtures cannot cause link following, secret disclosure, configuration changes, or unrelated tool calls.

### 15. Complete packaging, CI, documentation, and release gates

- Add Docker and native packaging, non-root/read-only runtime behavior, cache volume, pinned images/actions, security policy, dependency updates, license notices, scans, SBOMs, signatures, provenance, and complete user/developer documentation.

**Verification:** Native and Docker stdio smoke tests pass in Codex and Claude. All target binaries and multi-architecture images build. Secret, vulnerability, and license scans pass. Artifacts include verifiable checksums, SBOMs, signatures, and attestations. Publishing remains disabled until the STRATZ clearance gate passes.

## Release-candidate acceptance test

Start both native and Docker servers, discover all tools/resources/prompts, exercise representative success and error calls, validate every result against the generated schemas, check cache persistence and purge behavior, verify cursor integrity, and confirm no secret appears in stdout, stderr, SQLite, fixtures, or artifacts.

## Assumptions

- No fetched STRATZ schema or constants are committed before redistribution clearance.
- Public contracts remain frozen at `1.0.0-draft.2`; infeasible fields require an explicit contract revision.
- HTTP transport, mutations, subscriptions, telemetry, hosted operation, and subjective coaching remain outside v1.
- SDK selection is based on the official Go SDK compatibility table and v1.6.1 API documentation.
