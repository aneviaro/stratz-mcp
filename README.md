# STRATZ MCP

Unofficial, local-only MCP server for bounded access to the STRATZ GraphQL API.

## Install

Build from a source checkout:

```sh
git clone <public-source-remote> stratz-mcp
cd stratz-mcp
make build
```

The executable is written to `./dist/stratz-mcp`. Source builds require Go 1.25 or newer, Make, and Git.

This repository does not publish official binaries, archives, containers, or release tags. Build from source.

Before copying the source tree into a public repository, run `make public-readiness`. The clean-history import flow is documented in [docs/public-repo-import.md](docs/public-repo-import.md).

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

Docker images are not published by this repository. If you build a local image yourself, use `docker` as the command and arguments equivalent to:

```sh
docker run --rm -i --read-only -e STRATZ_API_TOKEN -v stratz-cache:/cache LOCAL_IMAGE serve
```

To avoid an environment secret, mount a token file read-only:

```sh
docker run --rm -i --read-only \
  -v "$HOME/.config/stratz/token:/run/secrets/stratz-token:ro" \
  -v stratz-cache:/cache \
  LOCAL_IMAGE --token-file /run/secrets/stratz-token serve
```

Do not also set `STRATZ_API_TOKEN`.

## MCP capabilities

The server exposes 15 tools covering players, matches, heroes, constants, leagues, live matches, server information, and guarded raw GraphQL. It also exposes local schema/constants resources and five generated prompts/portable skills. See:

- [Tool reference](docs/generated-tool-contracts.md)
- [Resources and prompts](docs/resources-prompts-skills.md)
- [Cache operations](docs/cache.md)
- [CLI reference](docs/cli.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Development guide](docs/development.md)

All MCP traffic is stdio. Stdout is reserved for JSON-RPC; diagnostics go to stderr.

## Docker

Local images should run as UID/GID 65532 with a read-only root filesystem and `/cache` as the persistent writable volume. See [development](docs/development.md) for the local Docker smoke sequence.

## Security and status

Do not publish fetched schema snapshots, constants, cache data, or local build artifacts. See [SECURITY.md](SECURITY.md).
