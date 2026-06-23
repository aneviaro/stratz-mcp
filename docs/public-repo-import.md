# Public repository import

A public source import copies the current tracked source snapshot into a new Git repository with clean history and a single initial commit. It is separate from package and image publication. Public tags, archives, and container images stay blocked until `go run ./cmd/release-clearance-check` succeeds and the reviewed clearance record allows publishing.

## Required checks

Run these commands from a clean worktree before exporting:

```sh
make public-readiness
make verify-public-surface
make check
make interop-smoke
```

Run the Docker smoke sequence as well:

```sh
mkdir -p dist/image/cache
touch dist/image/cache/.keep
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/image/stratz-mcp-linux-amd64 ./cmd/stratz-mcp
docker build --build-arg TARGETARCH=amd64 -t stratz-mcp:test .
CLIENT_PROFILE=codex ./scripts/interop-smoke.sh docker stratz-mcp:test
CLIENT_PROFILE=claude ./scripts/interop-smoke.sh docker stratz-mcp:test
```

These checks verify public documentation and release disclosures, the registered MCP tools/resources/prompts, generated portable skills, generated installation docs, native protocol behavior, and Docker protocol behavior.

## Content that must stay out of the public import

Do not carry over:

- private planning notes or local working material
- developer-specific home-path examples or other machine-local state
- dotenv files, token files, build outputs, tool caches, and SQLite cache data
- fetched schema snapshots, fetched constants, or any other restricted STRATZ-derived local artifacts
- the existing `.git` directory or any private commit history

## Manual clean-history import

Export the tracked `HEAD` snapshot into a fresh directory, review it, and create a new repository only after the checks above pass:

```sh
test -z "$(git status --short)"
export_dir="$(mktemp -d)"
mkdir -p "$export_dir/stratz-mcp"
git archive --format=tar HEAD | tar -xf - -C "$export_dir/stratz-mcp"
cd "$export_dir/stratz-mcp"
git init
git add .
git commit -m "Initial public import"
make public-readiness
make verify-public-surface
make check
```

Review the exported tree before `git add .`. If any private working files, build outputs, or restricted STRATZ artifacts were copied across, remove them and restart the export.

## Push the public source repository

```sh
git remote add origin <public-source-remote>
git branch -M main
git push -u origin main
```

Only the source repository should be pushed at this stage. Do not create release tags or publish packages or images until the clearance check succeeds and the protected release workflow is allowed to run.
