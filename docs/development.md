# Development

Prerequisites: Go 1.25+, Git, and Docker for image checks. `make check` runs module verification, formatting, vet, tests, deterministic-generation checks, restricted-artifact checks, release policies, and build metadata validation.

Useful targets:

```sh
make generate
make notices
make build
make cross-build
make package VERSION=v1.0.0 REVISION="$(git rev-parse HEAD)"
make interop-smoke
make test
make vet
make verify
make check-format
make check-generated
make check-restricted
make check-policies
make verify-build-info
make tools
```

Canonical generation ownership:

- `docs/tool-contracts.json` generates contracts, schemas, examples, protocol fixtures, and the tool reference.
- `internal/graphql/operations/*.graphql` plus the bootstrap schema generate genqlient code.
- `workflows/workflows.json` generates MCP prompts, portable skills, and installation guidance.

Generated files are not edited directly. `VERSION`, `REVISION`, and `SCHEMA_VERSION` are injected through linker flags; a valid local `schema/manifest.json` overrides the build schema value at runtime.

The MCP package is a stdio adapter; domain packages own normalization and pagination; `internal/stratz` owns bounded HTTP; raw GraphQL is default-deny. The production endpoint remains fixed and executors are injected only at test boundaries.

Never commit `.env`, tokens, cache databases, introspection responses, fetched schemas, fetched constants, or `.stratz-restricted`.

CI additionally runs vulnerability, license, secret, race, native packaging, Docker, SBOM, and interoperability jobs. Dependency updates are proposed weekly. Security updates can bypass the normal cadence; exceptions require an owner, rationale, compensating controls, and expiry.
