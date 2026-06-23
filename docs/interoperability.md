# Interoperability checks

Treat interoperability status as current only when the repeatable smoke commands below pass against the working tree being reviewed.

## Native stdio smoke

```sh
make build
CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native dist/stratz-mcp
CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native dist/stratz-mcp
```

## Docker stdio smoke

```sh
mkdir -p dist/image/cache
touch dist/image/cache/.keep
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/image/stratz-mcp-linux-amd64 ./cmd/stratz-mcp
docker build --build-arg TARGETARCH=amd64 -t stratz-mcp:test .
CLIENT_PROFILE=codex ./scripts/interop-smoke.sh docker stratz-mcp:test
CLIENT_PROFILE=claude ./scripts/interop-smoke.sh docker stratz-mcp:test
```

These checks validate initialization, exact tool/resource/prompt discovery, `stratz_server_info`, Draft 2020-12 schema publication, JSON-RPC-only stdout, and credential non-disclosure.

They are deterministic protocol compatibility checks, not claims that a locally installed proprietary client UI was controlled in this repository environment. Real installed-client native and Docker checks remain a protected release-environment responsibility before approval.
