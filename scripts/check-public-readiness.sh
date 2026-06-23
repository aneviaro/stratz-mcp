#!/usr/bin/env bash
set -euo pipefail

repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
repo_root="$(cd "$repo_root" && pwd)"
cd "$repo_root"

if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "public-readiness audit must run inside a git repository" >&2
  exit 1
fi

tracked_worktree_files() {
  while IFS= read -r path; do
    if [[ -e "$path" ]]; then
      printf '%s\n' "$path"
    fi
  done < <(git ls-files)
}

REPO_ROOT="$repo_root" "$repo_root/scripts/check-restricted-artifacts.sh"

forbidden_paths='(^|/)\.ralphex(/|$)|(^|/)docs/plans(/|$)|(^|/)docs/implementation-plan\.md$|(^|/)\.env$|(^|/)dist(/|$)|(^|/)\.bin(/|$)|(^|/)\.stratz-local(/|$)|(^|/)cache\.db(-wal|-shm)?$'
violations="$(tracked_worktree_files | grep -E "$forbidden_paths" || true)"
if [[ -n "$violations" ]]; then
  echo "tracked private/local files are present:" >&2
  echo "$violations" >&2
  exit 1
fi

machine_local_prefix="$(printf '/%s/%s' 'Users' 'alex')"
local_path_hits="$(git --no-pager grep -nI --fixed-strings "$machine_local_prefix" -- . ':(exclude)README.md' ':(exclude)docs/**' || true)"
if [[ -n "$local_path_hits" ]]; then
  echo "tracked non-doc files contain machine-local absolute home paths:" >&2
  echo "$local_path_hits" >&2
  exit 1
fi

if ! grep -Fq 'Unofficial, local-only MCP server' README.md; then
  echo "README.md must describe the project as unofficial" >&2
  exit 1
fi

if ! grep -Fq 'Public release is currently blocked' README.md; then
  echo "README.md must disclose that public release remains blocked" >&2
  exit 1
fi

if ! grep -Fq 'Public publishing is disabled until `go run ./cmd/release-clearance-check` succeeds against `docs/release-clearance.json`.' docs/release.md; then
  echo "docs/release.md must disclose the STRATZ release-clearance block" >&2
  exit 1
fi
