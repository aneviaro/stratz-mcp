# STRATZ MCP

Unofficial, local-only MCP server for bounded access to the STRATZ GraphQL API. Public release is currently blocked pending explicit STRATZ API-use, caching, redistribution, attribution, and branding clearance.

## Install

During the clearance hold, build from source:

```sh
go build -o stratz-mcp ./cmd/stratz-mcp
```

After clearance, signed native archives and the multi-architecture image `ghcr.io/aneviaro/stratz-mcp` are the supported distribution channels. Go 1.25 or newer is required for source builds.

## Configure

Set exactly one credential source:

```sh
export STRATZ_API_TOKEN='...'
./stratz-mcp doctor
./stratz-mcp serve
```

Alternatively use `--token-file`, or an explicit `--env-file`. Configuration precedence is CLI, environment, explicit YAML, defaults. Run `stratz-mcp help` for global options. Tokens are never accepted in YAML.

Common environment variables include `STRATZ_CACHE_ENABLED`, `STRATZ_CACHE_DIRECTORY`, `STRATZ_LOG_LEVEL`, and `STRATZ_LOG_FORMAT`. See [docs/configuration.md](docs/configuration.md).

## MCP capabilities

The server exposes 15 tools covering players, matches, heroes, constants, leagues, live matches, server information, and guarded raw GraphQL. It also exposes local schema/constants resources and five generated prompts/portable skills. See:

- [Tool reference](docs/generated-tool-contracts.md)
- [Resources and prompts](docs/resources-prompts-skills.md)
- [Cache operations](docs/cache.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Developer and release guide](docs/development.md)

All MCP traffic is stdio. Stdout is reserved for JSON-RPC; diagnostics go to stderr.

## Docker

The release image runs as UID/GID 65532 with a read-only root filesystem and `/cache` as its only persistent writable volume:

```sh
docker run --rm -i --read-only \
  -e STRATZ_API_TOKEN \
  -v stratz-cache:/cache \
  ghcr.io/aneviaro/stratz-mcp:v1.0.0 serve
```

## Security and status

Do not publish fetched schema snapshots, constants, cache data, or release artifacts while the clearance record is blocked. See [SECURITY.md](SECURITY.md), [docs/release.md](docs/release.md), and [docs/release-clearance.json](docs/release-clearance.json).
