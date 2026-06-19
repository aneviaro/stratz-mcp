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
```

Generated contracts, GraphQL operations, prompts, and skills must be regenerated and committed together with their canonical sources. Never commit `.env`, tokens, cache databases, introspection responses, fetched schemas, or fetched constants.

CI additionally runs vulnerability, license, secret, race, native packaging, Docker, SBOM, and interoperability jobs. Dependency updates are proposed weekly. Security updates can bypass the normal cadence; exceptions require an owner, rationale, compensating controls, and expiry.
