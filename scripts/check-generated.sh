#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
snapshot="$(mktemp -d)"
trap 'rm -rf "$snapshot"' EXIT

cd "$repo_root"

cp -R internal/contracts/generated "$snapshot/generated"
cp internal/contracts/zz_generated.contracts.go "$snapshot/zz_generated.contracts.go"
cp docs/generated-tool-contracts.md "$snapshot/generated-tool-contracts.md"
cp internal/graphql/generated/operations.go "$snapshot/operations.go"
cp internal/graphql/generated/operations.json "$snapshot/operations.json"
cp internal/prompts/zz_generated.prompts.go "$snapshot/prompts.go"
cp -R skills "$snapshot/skills"
cp docs/skills-installation.md "$snapshot/skills-installation.md"

go generate ./...

diff -ru "$snapshot/generated" internal/contracts/generated
diff -u "$snapshot/zz_generated.contracts.go" internal/contracts/zz_generated.contracts.go
diff -u "$snapshot/generated-tool-contracts.md" docs/generated-tool-contracts.md
diff -u "$snapshot/operations.go" internal/graphql/generated/operations.go
diff -u "$snapshot/operations.json" internal/graphql/generated/operations.json
diff -u "$snapshot/prompts.go" internal/prompts/zz_generated.prompts.go
diff -ru "$snapshot/skills" skills
diff -u "$snapshot/skills-installation.md" docs/skills-installation.md
