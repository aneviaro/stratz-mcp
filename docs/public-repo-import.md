# Public repository import

A public source import copies the current tracked source snapshot into a new Git repository with clean history and a single initial commit. This repository does not publish official binaries, archives, containers, or release tags; build from source.

## Required checks

Run these commands from a clean worktree before exporting:

```sh
make public-readiness
make check
```

Optional MCP runtime checks:

```sh
make interop-smoke
```

Optional Docker smoke sequence:

```sh
mkdir -p dist/image/cache
touch dist/image/cache/.keep
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/image/stratz-mcp-linux-amd64 ./cmd/stratz-mcp
docker build --build-arg TARGETARCH=amd64 -t stratz-mcp:test .
CLIENT_PROFILE=codex ./scripts/interop-smoke.sh docker stratz-mcp:test
CLIENT_PROFILE=claude ./scripts/interop-smoke.sh docker stratz-mcp:test
```

The required checks verify public-import safety, generated artifacts, formatting, vet, tests, and build metadata. The optional smoke checks verify native and Docker MCP protocol behavior.

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

The script archives the tracked `HEAD` snapshot, removes the explicit public-import denylist, initializes a fresh repository, creates a single initial commit, renames the branch to `main`, and reruns `make public-readiness` and `make check` inside the generated repository before returning.

By default the script refuses dirty tracked or untracked inputs. Use the documented override only when you intentionally want to ignore dirty files and export the tracked `HEAD` snapshot:

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

Only the source repository should be pushed. Do not create release tags or publish packages or images from this repository.
