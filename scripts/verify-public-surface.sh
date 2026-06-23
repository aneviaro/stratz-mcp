#!/usr/bin/env bash
set -euo pipefail

repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
repo_root="$(cd "$repo_root" && pwd)"
go_cmd="${GO:-go}"
target="${1:-dist/stratz-mcp}"

if [[ "$target" != /* ]]; then
	target="$repo_root/$target"
fi

cd "$repo_root"

if [[ ! -x "$target" ]]; then
	echo "public-surface verification requires an executable server binary at $target" >&2
	exit 1
fi

"$go_cmd" test ./internal/mcp ./internal/releasepack
./scripts/check-generated.sh
CLIENT_PROFILE=codex ./scripts/interop-smoke.sh native "$target"
CLIENT_PROFILE=claude ./scripts/interop-smoke.sh native "$target"
