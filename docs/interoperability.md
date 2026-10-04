# Interoperability checks

Treat interoperability status as current only when the repeatable smoke commands below pass against the working tree being reviewed.

The smoke harness exercises stdio only. It intentionally does not validate Streamable HTTP, HTTP routing headers, OAuth behavior, sticky routing, or horizontal HTTP deployment.

## Native stdio smoke

```sh
make build
MCP_PROTOCOL_PROFILE=modern CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp
MCP_PROTOCOL_PROFILE=legacy CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp
MCP_PROTOCOL_PROFILE=modern CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp
MCP_PROTOCOL_PROFILE=legacy CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp
```

## Docker stdio smoke

```sh
mkdir -p dist/image/cache
touch dist/image/cache/.keep
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/image/stratz-mcp-linux-amd64 ./cmd/stratz-mcp
docker build --build-arg TARGETARCH=amd64 -t stratz-mcp:test .
MCP_PROTOCOL_PROFILE=modern CLIENT_PROFILE=codex ./scripts/interop-smoke.sh docker stratz-mcp:test
MCP_PROTOCOL_PROFILE=legacy CLIENT_PROFILE=codex ./scripts/interop-smoke.sh docker stratz-mcp:test
MCP_PROTOCOL_PROFILE=modern CLIENT_PROFILE=claude ./scripts/interop-smoke.sh docker stratz-mcp:test
MCP_PROTOCOL_PROFILE=legacy CLIENT_PROFILE=claude ./scripts/interop-smoke.sh docker stratz-mcp:test
```

The modern profile validates MCP `2026-07-28` per-request protocol metadata, `server/discover`, advertised protocol versions, cache hints, exact v2 discovery of the seven tools, explicit query-mode schemas, `stratz_server_info`, Draft 2020-12 schema publication, JSON-RPC-only stdout, and credential non-disclosure. Superseded v1 names are absent and are not compatibility aliases.

The legacy profile validates the `2025-11-25` `initialize` plus `notifications/initialized` lifecycle, the same seven-tool v2 catalog and `stratz_server_info` behavior, and compatibility with clients that do not use modern per-request metadata. Clients must rediscover the catalog after the breaking cutover; v1 cursors are invalid and v2 caches start cold.

These are deterministic protocol compatibility checks, not claims that a locally installed proprietary client UI was controlled in this repository environment.
