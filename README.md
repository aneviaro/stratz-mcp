# STRATZ MCP

Unofficial, local-only MCP server for bounded access to the STRATZ GraphQL API. Public release is currently blocked pending explicit STRATZ API-use, caching, redistribution, attribution, and branding clearance.

## Install

Public images and archives are not published while clearance is blocked. Build from a source checkout:

```sh
git clone <public-source-remote> stratz-mcp
cd stratz-mcp
make build
```

The executable is written to `./dist/stratz-mcp`. Source builds require Go 1.25 or newer, Make, and Git.

After clearance, signed native archives and the multi-architecture image `ghcr.io/aneviaro/stratz-mcp` become the supported distribution channels.

Before copying the source tree into a public repository, run `make public-readiness` and `make verify-public-surface`. The clean-history import flow is documented in [docs/public-repo-import.md](docs/public-repo-import.md).

## Configure

Set exactly one credential source:

```sh
export STRATZ_API_TOKEN='...'
./dist/stratz-mcp doctor
./dist/stratz-mcp serve
```

Alternatively use `--token-file`, or an explicit `--env-file`. Configuration precedence is CLI, environment, explicit YAML, defaults. Run `stratz-mcp help` for global options. Tokens are never accepted in YAML.

Common environment variables include `STRATZ_CACHE_ENABLED`, `STRATZ_CACHE_DIR`, `STRATZ_LOG_LEVEL`, and `STRATZ_LOG_FORMAT`. See [configuration](docs/configuration.md) and the [CLI reference](docs/cli.md).

## Connect an MCP client

The `command` value must be the absolute path to the built executable, not the
repository directory. For example, if the repository is checked out at
`/path/to/stratz-mcp`, use `/path/to/stratz-mcp/dist/stratz-mcp`.

The examples below forward `STRATZ_API_TOKEN` from the MCP client's environment.
Export it before starting the client from that same shell:

```sh
export STRATZ_API_TOKEN='...'
codex
```

Codex native configuration in `~/.codex/config.toml`:

```toml
[mcp_servers.stratz]
command = "/path/to/stratz-mcp/dist/stratz-mcp"
args = ["serve"]
# Forward the existing variable; this does not set its value.
env_vars = ["STRATZ_API_TOKEN"]
```

Claude native configuration in `.mcp.json`:

```json
{
  "mcpServers": {
    "stratz": {
      "command": "/path/to/stratz-mcp/dist/stratz-mcp",
      "args": ["serve"],
      "env": {"STRATZ_API_TOKEN": "${STRATZ_API_TOKEN}"}
    }
  }
}
```

If the client does not inherit your shell environment, store the token in a
private dotenv file and pass its absolute path explicitly:

```dotenv
STRATZ_API_TOKEN=...
```

```toml
[mcp_servers.stratz]
command = "/path/to/stratz-mcp/dist/stratz-mcp"
args = ["serve", "--env-file", "/absolute/path/to/.env"]
```

Do not commit the dotenv file. Use only one credential source: either forward
`STRATZ_API_TOKEN` or pass `--env-file`, not both. Restart the MCP client after
changing its configuration or environment.

For Docker, use `docker` as the command and arguments equivalent to:

```sh
docker run --rm -i --read-only -e STRATZ_API_TOKEN -v stratz-cache:/cache IMAGE serve
```

Replace `IMAGE` with the approved release image, such as `ghcr.io/aneviaro/stratz-mcp:<version>`, only after clearance.

To avoid an environment secret, mount a token file read-only:

```sh
docker run --rm -i --read-only \
  -v "$HOME/.config/stratz/token:/run/secrets/stratz-token:ro" \
  -v stratz-cache:/cache \
  IMAGE --token-file /run/secrets/stratz-token serve
```

Do not also set `STRATZ_API_TOKEN`.

## MCP capabilities

The server exposes 15 tools covering players, matches, heroes, constants, leagues, live matches, server information, and guarded raw GraphQL. It also exposes local schema/constants resources and five generated prompts/portable skills. See:

- [Tool reference](docs/generated-tool-contracts.md)
- [Resources and prompts](docs/resources-prompts-skills.md)
- [Cache operations](docs/cache.md)
- [CLI reference](docs/cli.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Developer and release guide](docs/development.md)

All MCP traffic is stdio. Stdout is reserved for JSON-RPC; diagnostics go to stderr.

## Docker

After clearance, the release image will run as UID/GID 65532 with a read-only root filesystem and `/cache` as its only persistent writable volume:

```sh
docker run --rm -i --read-only \
  -e STRATZ_API_TOKEN \
  -v stratz-cache:/cache \
  IMAGE serve
```

Replace `IMAGE` with `ghcr.io/aneviaro/stratz-mcp:<version>` only after clearance.

## Security and status

Do not publish fetched schema snapshots, constants, cache data, or release artifacts while the clearance record is blocked. See [SECURITY.md](SECURITY.md), [docs/release.md](docs/release.md), and [docs/release-clearance.json](docs/release-clearance.json).
