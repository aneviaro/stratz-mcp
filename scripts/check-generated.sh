#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
snapshot="$(mktemp -d)"
trap 'rm -rf "$snapshot"' EXIT

cd "$repo_root"

cp -R internal/contracts/generated "$snapshot/generated"
cp internal/contracts/zz_generated.contracts.go "$snapshot/zz_generated.contracts.go"
cp docs/generated-tool-contracts.md "$snapshot/generated-tool-contracts.md"

go generate ./...

diff -ru "$snapshot/generated" internal/contracts/generated
diff -u "$snapshot/zz_generated.contracts.go" internal/contracts/zz_generated.contracts.go
diff -u "$snapshot/generated-tool-contracts.md" docs/generated-tool-contracts.md
