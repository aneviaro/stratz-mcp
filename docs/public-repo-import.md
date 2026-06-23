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

## Automated clean-history import

Generate the clean public repository outside the working tree so the source checkout stays clean:

```sh
make public-import OUTPUT_DIR="$(mktemp -d)/public-import"
```

The script archives the tracked `HEAD` snapshot, removes the explicit public-import denylist, initializes a fresh repository, creates a single initial commit, renames the branch to `main`, and reruns `make public-readiness`, `make verify-public-surface`, and `make check` inside the generated repository before returning.

By default the script refuses dirty tracked or untracked inputs. Use the documented override only when you intentionally want to snapshot a dirty tree:

```sh
ALLOW_DIRTY=1 make public-import OUTPUT_DIR="$(mktemp -d)/public-import"
```

If you prefer the default destination inside the source repository, run:

```sh
make public-import
```

This writes the generated repository to `dist/public-import`.

## Push the public source repository

```sh
export_dir="$(mktemp -d)"
make public-import OUTPUT_DIR="$export_dir/public-import"
cd "$export_dir/public-import"
git remote add origin <public-source-remote>
git push -u origin main
```

Only the source repository should be pushed at this stage. Do not create release tags or publish packages or images until the clearance check succeeds and the protected release workflow is allowed to run.
