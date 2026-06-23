# Development

Prerequisites: Go 1.25+, Git, and Docker for image checks. `make check` runs module verification, formatting, vet, tests, deterministic-generation checks, public-readiness checks, public-surface verification, release-policy checks, and build metadata validation.

Useful targets:

```sh
make generate
make notices
make build
make cross-build
make package VERSION=v1.0.0 REVISION="$(git rev-parse HEAD)"
make interop-smoke
make public-readiness
make verify-public-surface
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

`make public-readiness` audits the tracked source tree for private working material, local-only artifacts, restricted STRATZ data, and missing public disclosures.

`make verify-public-surface` builds the server, runs the focused MCP and release-pack tests, checks generated artifacts, and exercises the Codex and Claude native interoperability smoke profiles.

Use this repeatable Docker smoke sequence before changing packaging, Docker, or release documentation:

```sh
mkdir -p dist/image/cache
touch dist/image/cache/.keep
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/image/stratz-mcp-linux-amd64 ./cmd/stratz-mcp
docker build --build-arg TARGETARCH=amd64 -t stratz-mcp:test .
CLIENT_PROFILE=codex ./scripts/interop-smoke.sh docker stratz-mcp:test
CLIENT_PROFILE=claude ./scripts/interop-smoke.sh docker stratz-mcp:test
```

Before preparing a public source import, run `make public-readiness`, `make verify-public-surface`, `make interop-smoke`, and the Docker smoke sequence above. Then follow [public repository import](public-repo-import.md) to create a clean-history repository snapshot for the public remote.

Canonical generation ownership:

- `docs/tool-contracts.json` generates contracts, schemas, examples, protocol fixtures, and the tool reference.
- `internal/graphql/operations/*.graphql` plus the bootstrap schema generate genqlient code.
- `workflows/workflows.json` generates MCP prompts, portable skills, and installation guidance.

Generated files are not edited directly. `VERSION`, `REVISION`, and `SCHEMA_VERSION` are injected through linker flags; a valid local `schema/manifest.json` overrides the build schema value at runtime.

The MCP package is a stdio adapter; domain packages own normalization and pagination; `internal/stratz` owns bounded HTTP; raw GraphQL is default-deny. The production endpoint remains fixed and executors are injected only at test boundaries.

Never commit `.env`, tokens, cache databases, introspection responses, fetched schemas, fetched constants, or `.stratz-restricted`.

Tagged releases, signed archives, and published images remain blocked until `go run ./cmd/release-clearance-check` succeeds against the reviewed clearance record.

CI additionally runs vulnerability, license, secret, race, native packaging, Docker, SBOM, and interoperability jobs. Dependency updates are proposed weekly. Security updates can bypass the normal cadence; exceptions require an owner, rationale, compensating controls, and expiry.
