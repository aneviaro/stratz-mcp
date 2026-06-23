#!/usr/bin/env bash
set -euo pipefail

repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
repo_root="$(cd "$repo_root" && pwd)"
cd "$repo_root"

tracked_worktree_files() {
  while IFS= read -r path; do
    if [[ -e "$path" ]]; then
      printf '%s\n' "$path"
    fi
  done < <(git ls-files)
}

forbidden='(^|/)(introspection\.json|\.stratz-restricted)$|(^|/)schema/(full|player|match|hero|league|live|constants)\.graphql$|(^|/)constants/(heroes|items|abilities|game-modes|regions|ranks)\.json$'

violations="$(tracked_worktree_files | grep -E "$forbidden" || true)"
if [[ -n "$violations" ]]; then
  echo "restricted STRATZ artifacts are tracked:" >&2
  echo "$violations" >&2
  exit 1
fi

if git --no-pager grep -l 'Generated from authenticated STRATZ introspection' -- '*.graphql' '*.json' 2>/dev/null; then
  echo "tracked files contain restricted authenticated STRATZ schema data" >&2
  exit 1
fi

if git --no-pager grep -l '"source": "authenticated STRATZ GraphQL introspection"' -- '*.json' 2>/dev/null; then
  echo "tracked JSON contains restricted authenticated STRATZ schema data" >&2
  exit 1
fi
