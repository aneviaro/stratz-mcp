---
Created: 2026-06-18
Purpose: Define the dependency-ordered, testable implementation plan for STRATZ MCP v1.
Status: Local implementation complete; manual release-candidate validation and STRATZ public-release clearance remain open
---

# STRATZ MCP v1 Implementation Plan

## Summary

The dependency-ordered milestones are implemented locally. Automated checks cover the code paths; Docker-daemon and installed-client acceptance evidence must still be recorded in the protected release environment. Publishing remains blocked.

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

## Implementation tasks

### Task 1: Close discovery and release-gate evidence

- Complete conservative fixtures or safe live probes for private profiles, partial GraphQL results, timeout, oversized response, and invalid credentials.
- Record STRATZ permission decisions for API wrapping, caching, schema/constants redistribution, attribution, and branding.

- [x] Run or fixture every remaining safe discovery probe.
- [x] Record dated, redacted evidence and resulting error mappings.
- [x] Document STRATZ permission decisions and source references.
- [x] Add a machine-checkable release-clearance record.
- [x] Add a test that blocks publishing while required clearance is missing.

**Verification:** Every discovery-matrix row has dated evidence or an explicitly documented mock-based policy. A release-gate test fails unless all required clearance fields are approved.

### Task 2: Establish project structure and dependency baseline

- Add the production command, recommended internal packages, pinned dependencies, generation entrypoints, and build-version injection.
- Retain existing discovery commands as development utilities.

- [x] Create `cmd/stratz-mcp` and the approved internal package skeleton.
- [x] Pin the MCP, GraphQL, configuration, SQLite, and compression dependencies.
- [x] Add build-time version, revision, and schema-version injection.
- [x] Add generation commands and developer entrypoints.
- [x] Add the Go 1.25/1.26 test matrix and native cross-build matrix.

**Verification:** `go mod verify`, `go vet ./...`, and `go test ./...` pass on Go 1.25 and 1.26. A minimal cross-compile succeeds for every native target.

### Task 3: Generate the public contract

- Build a deterministic generator that validates and dereferences `tool-contracts.json`, then emits Go contract types, embedded schemas, validators, protocol fixtures, and reference documentation.
- Reject unsupported schema constructs instead of weakening validation.

- [x] Parse and validate the contract registry and contract version.
- [x] Dereference shared definitions for every tool schema.
- [x] Generate Go request, response, and error types.
- [x] Generate embedded schemas, validators, examples, and protocol fixtures.
- [x] Add deterministic generation and stale-artifact CI checks.

**Verification:** All 15 tools have Draft 2020-12 input/output schemas. Generated examples validate. `go generate ./...` produces no diff on a clean tree.

### Task 4: Implement configuration, credentials, and safe logging

- Add strict CLI/environment/YAML precedence, explicit dotenv loading, mutually exclusive secret sources, bounded token-file parsing, permission diagnostics, and centralized redaction.
- Implement `version` and command help behavior.

- [x] Define strict configuration types, defaults, and validation.
- [x] Implement CLI, environment, explicit YAML, and explicit dotenv loading.
- [x] Implement mutually exclusive environment and file token sources.
- [x] Add token/config/cache permission checks for `doctor`.
- [x] Implement stderr logging with centralized secret and header redaction.
- [x] Implement root help and `version`.

**Verification:** Table-driven tests cover precedence, unknown YAML keys, conflicting sources, symlinks, multiline/NUL/oversized tokens, absent credentials, and redaction of every sensitive header.

### Task 5: Build the bounded STRATZ HTTP client

- Implement fixed-endpoint POST requests, required headers, gzip streaming, redirect refusal, decompressed-size limits, cancellation, retries, WAF detection, rate-window parsing, and stable error mapping.
- Enforce the five-round-trip budget per MCP call.

- [x] Define the request executor and injectable test transport.
- [x] Implement fixed endpoint, authentication, media types, user agent, and gzip.
- [x] Add bounded wire, decompressed, and error-body readers.
- [x] Implement WAF, HTTP, GraphQL, TLS, network, and timeout classification.
- [x] Parse sanitized multi-window rate-limit metadata.
- [x] Add bounded jittered retries and per-call request accounting.

**Verification:** Mock-server tests cover every HTTP/network mapping, gzip bombs, malformed JSON, redirects, partial results, WAF HTML, retry timing, cancellation, and request-budget exhaustion without leaking response bodies or sensitive headers.

### Task 6: Implement MCP lifecycle and wire behavior

- Register static tools, resources, and prompts over stdio using the official SDK.
- Create one result encoder that emits authoritative `structuredContent`, its exact compact JSON text mirror, correct `outputSchema`, and execution-error `isError`.
- Add `doctor` and `stratz_server_info`.

- [x] Create the application composition root and stdio server.
- [x] Register static capabilities and generated tool schemas.
- [x] Implement shared success and execution-error result encoding.
- [x] Implement protocol-safe stderr diagnostics and shutdown.
- [x] Implement `doctor` and `stratz_server_info`.
- [x] Add an SDK conformance and raw stdio protocol harness.

**Verification:** Protocol tests negotiate `2025-11-25`, reject pre-initialization calls, keep stdout JSON-RPC-only, distinguish protocol and tool errors, and byte-compare the text mirror against compact `structuredContent`.

### Task 7: Implement raw GraphQL policy and execution

- Parse and validate one query operation; expand fragments; enforce default-deny roots, introspection policy, variable limits, depth, aliases, field counts, list bounds, nested lists, complexity, request budget, and response size.
- Preserve bounded upstream `data`, `errors`, and `extensions`; mark partial raw responses.

- [x] Parse documents and select exactly one named or unambiguous operation.
- [x] Expand fragments with cycle detection and worst-case directive charging.
- [x] Enforce operation, root-field, alias, introspection, and schema policies.
- [x] Enforce document, variable, list, depth, field, and complexity limits.
- [x] Implement canonical raw execution and bounded response preservation.
- [x] Add cacheability and sensitive-field policy hooks with caching disabled by default.

**Verification:** Adversarial tests cover aliases, fragments, cycles, directives, denied/unknown roots, mutations, subscriptions, introspection, unbounded lists, variable-supplied limits, and every raw-query error code.

### Task 8: Implement schema lifecycle and resources

- Add authenticated deterministic `schema pull`, schema hashing, domain subset generation, local-only generated artifacts before redistribution clearance, and all required schema/constants resource handlers.

- [x] Implement authenticated introspection through the production HTTP contract.
- [x] Normalize and serialize schema snapshots deterministically.
- [x] Generate schema hashes, domain subsets, and validation metadata.
- [x] Configure and generate curated operations with `genqlient`.
- [x] Register all required schema and constants resource URIs.
- [x] Add safeguards against publishing restricted generated data.

**Verification:** Fixture-based schema pulls are deterministic. Generated operations compile. Resource lists and reads match expected URIs. CI detects schema drift and accidentally committed restricted upstream artifacts.

### Task 9: Implement pagination and batch primitives

- Add canonical filter hashing, HKDF/HMAC cursor signing, token/tool/version binding, expiry classes, bounded scan continuation, deduplication, ordering reconstruction, cancellation, and atomic batch errors.

- [x] Define and canonically encode the versioned cursor payload.
- [x] Derive cursor signing keys and implement authenticated encoding.
- [x] Validate expiry, tool, filters, token namespace, schema, and operation versions.
- [x] Implement reusable bounded client-side scan continuation.
- [x] Implement batch validation, deduplication, cancellation, and reconstruction.
- [x] Track and enforce the shared upstream request budget.

**Verification:** Tests cover tampering, wrong filters/tools/tokens, rotation, expiry, restart stability, duplicate inputs, first-failure cancellation, exact ordering, and the five-request ceiling for 25-item batches.

### Task 10: Implement SQLite caching

- Add migrations, WAL mode, busy timeout, token namespaces, cache classes, TTL/stale rules, asynchronous writes, Zstandard compression, LRU eviction, permissions, corruption fallback, and cache CLI commands.
- Disable raw caching until field classifications are approved.

- [x] Define the cache schema, migrations, indexes, and format version.
- [x] Implement secure database creation, WAL mode, and busy timeout.
- [x] Implement canonical keys, token namespaces, and cache classifications.
- [x] Implement reads, fresh bypass, TTL, stale fallback, and asynchronous writes.
- [x] Add compression, access tracking, size accounting, and LRU eviction.
- [x] Implement `cache stats` and transactional `cache clear` variants.
- [x] Add process-level fallback when cache initialization or operation fails.

**Verification:** Tests cover hit/miss/stale/fresh behavior, exclusions, namespace isolation, compression threshold, eviction, concurrent processes, lock/corruption fallback, permissions, symlink handling, statistics, and transactional clearing.

### Task 11: Implement player and match domains

- Add player identifier normalization, singular/list/batch player operations, match detail levels, normalized mappings, bounded minimum-duration scanning, and required `DATA_NOT_READY` context.

- [x] Implement account ID, SteamID64, and STRATZ profile URL normalization.
- [x] Author and generate player singular, list, and batch operations.
- [x] Author and generate match singular and batch operations by detail level.
- [x] Implement normalized player, match, player-event, and provenance mappings.
- [x] Implement bounded player-match filtering and cursor continuation.
- [x] Implement private, missing, partial, and `DATA_NOT_READY` error paths.

**Verification:** Contract fixtures cover account ID, SteamID64, URLs, private/missing players, list continuation, batch atomicity, parsed/unparsed matches, detail-level field inclusion, partial upstream failures, and raw bypass behavior.

### Task 12: Implement hero and constants domains

- Add cached hero lookup indexes, ambiguity handling, hero batches, constants retrieval, statistics bucket translation, denominator calculation, matchup/synergy mapping, and unsupported filter handling.

- [x] Author and generate constants and hero-statistics operations.
- [x] Build deterministic hero ID, localized-name, and slug indexes.
- [x] Implement singular and batch hero resolution with ambiguity errors.
- [x] Implement each constants type and explicit combined retrieval.
- [x] Translate requested ranges into bounded statistics buckets.
- [x] Normalize rates, breakdowns, matchups, synergies, warnings, and provenance.

**Verification:** Tests cover ID/name/slug lookup, ambiguous suggestions, duplicate batches, each constants type, explicit `all`, effective date ranges, patch incompatibilities, rate calculations, and absent rank redistribution data.

### Task 13: Implement league and live-match domains

- Add league retrieval/listing/matches, derived status, bounded text scans, live native filters, bounded client-side filters, and truthful incomplete-scan cursors.

- [x] Author and generate league singular, listing, and match operations.
- [x] Implement league normalization and deterministic status derivation.
- [x] Implement bounded league-name search and continuation.
- [x] Author and generate live-match operations with native filters and ordering.
- [x] Implement bounded team, player, mode, and spectator filtering.
- [x] Normalize incomplete scans without claiming snapshot completeness.

**Verification:** Tests cover native and client-side filters, five-page exhaustion, continuation, derived statuses, changing live data, sort modes, and confirmation that unsupported region filtering is absent.

### Task 14: Generate workflows, prompts, and portable skills

- Define one canonical workflow source and generate the five MCP prompts plus five `SKILL.md` packages with Codex and Claude adapters.
- Encode attribution, provenance, fact-versus-interpretation rules, and untrusted-content defenses.

- [x] Define the canonical workflow schema and five workflow definitions.
- [x] Generate and register MCP prompt templates and arguments.
- [x] Generate the five portable skill directories and `SKILL.md` files.
- [x] Add Codex and Claude installation guidance or adapters.
- [x] Add attribution, evidence, freshness, and insufficient-data rules.
- [x] Add untrusted-content and prompt-injection fixtures.

**Verification:** Generation is reproducible. Skill validation passes. Injection fixtures cannot cause link following, secret disclosure, configuration changes, or unrelated tool calls.

### Task 15: Complete packaging, CI, documentation, and release gates

- Add Docker and native packaging, non-root/read-only runtime behavior, cache volume, pinned images/actions, security policy, dependency updates, license notices, scans, SBOMs, signatures, provenance, and complete user/developer documentation.

- [x] Add native release builds, archives, checksums, and version metadata.
- [x] Add the pinned multi-stage, multi-architecture, non-root Docker image.
- [x] Add format, vet, test, generation, vulnerability, license, and secret checks.
- [x] Generate notices, SPDX/CycloneDX SBOMs, signatures, and provenance.
- [x] Add `SECURITY.md`, dependency-update policy, and release procedures.
- [x] Complete installation, configuration, tool, resource, prompt, skill, cache, and troubleshooting documentation.
- [x] Configure automated native protocol smoke checks for Codex and Claude profiles.
- [x] Configure automated Docker protocol smoke checks for Codex and Claude profiles.
- [ ] Execute and record native installed-client acceptance in Codex and Claude.
- [ ] Execute and record Docker installed-client acceptance in Codex and Claude.
- [x] Require the STRATZ clearance gate before public publishing jobs run.

**Verification:** Native and Docker stdio smoke tests pass in Codex and Claude. All target binaries and multi-architecture images build. Secret, vulnerability, and license scans pass. Artifacts include verifiable checksums, SBOMs, signatures, and attestations. Publishing remains disabled until the STRATZ clearance gate passes.

## Release-candidate acceptance test

**Status:** Automated native protocol checks pass locally. Docker protocol jobs are configured in CI; protected-environment results and native/Docker installed-client acceptance remain pending. See [the interoperability record](interoperability.md).

Start both native and Docker servers, discover all tools/resources/prompts, exercise representative success and error calls, validate every result against the generated schemas, check cache persistence and purge behavior, verify cursor integrity, and confirm no secret appears in stdout, stderr, SQLite, fixtures, or artifacts.

## Assumptions

- No fetched STRATZ schema or constants are committed before redistribution clearance.
- Public contracts are frozen at `1.0.0-draft.3`; infeasible fields require another explicit contract revision.
- HTTP transport, mutations, subscriptions, telemetry, hosted operation, and subjective coaching remain outside v1.
- SDK selection is based on the official Go SDK compatibility table and v1.6.1 API documentation.
