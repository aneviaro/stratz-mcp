# STRATZ MCP

Unofficial, local-only MCP server for bounded access to the STRATZ GraphQL API.

Ask your AI agent:
- “Analyze my last 10 Dota matches and find my most common throw pattern.”
- “Compare this player’s hero pool against the current meta.”
- “Summarize live pro matches with draft strengths and weaknesses.”
- “Find Dota matches where Team X lost after a 10k gold lead.”

## Install

Build from a source checkout:

```sh
git clone <public-source-remote> stratz-mcp
cd stratz-mcp
make build
```

The executable is written to `./dist/stratz-mcp`. Source builds require Go 1.25 or newer, Make, and Git.

This repository does not publish official binaries, archives, containers, or release tags. Build from source.

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

Docker images are not published by this repository. The MCP client only needs to run `docker run`; build an image only if the referenced image tag does not already exist locally:

```sh
ARCH=$(go env GOARCH)
mkdir -p dist/image/cache
touch dist/image/cache/.keep
CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH \
  go build -trimpath -o dist/image/stratz-mcp-linux-$ARCH ./cmd/stratz-mcp
docker build --build-arg TARGETARCH=$ARCH -t stratz-mcp:local .
```

Configure the MCP client to run `docker` over stdio. Keep `-i`; MCP traffic is line-delimited JSON-RPC on stdin/stdout. Do not publish a port.

Codex native configuration:

```toml
[mcp_servers.stratz]
command = "docker"
args = [
  "run", "--rm", "-i", "--read-only",
  "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m",
  "-e", "STRATZ_API_TOKEN",
  "-v", "stratz-cache:/cache",
  "stratz-mcp:local", "serve"
]
env_vars = ["STRATZ_API_TOKEN"]
```

Claude native configuration in `.mcp.json`:

```json
{
  "mcpServers": {
    "stratz": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i", "--read-only",
        "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m",
        "-e", "STRATZ_API_TOKEN",
        "-v", "stratz-cache:/cache",
        "stratz-mcp:local", "serve"
      ],
      "env": {"STRATZ_API_TOKEN": "${STRATZ_API_TOKEN}"}
    }
  }
}
```

If the client cannot inherit environment variables, pass a private dotenv file to Docker instead:

```toml
[mcp_servers.stratz]
command = "docker"
args = [
  "run", "--rm", "-i", "--read-only",
  "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m",
  "--env-file", "/absolute/path/to/.env",
  "-v", "stratz-cache:/cache",
  "stratz-mcp:local", "serve"
]
```

To avoid an environment secret entirely, mount a token file read-only:

```sh
docker run --rm -i --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  -v "$HOME/.config/stratz/token:/run/secrets/stratz-token:ro" \
  -v stratz-cache:/cache \
  stratz-mcp:local --token-file /run/secrets/stratz-token serve
```

Use exactly one credential source: forwarded `STRATZ_API_TOKEN`, Docker `--env-file`, or `--token-file`. Restart or reconnect the MCP client after changing Docker image tags, configuration, or environment.

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
