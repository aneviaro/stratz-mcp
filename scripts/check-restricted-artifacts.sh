#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

forbidden='(^|/)(introspection\.json|\.stratz-restricted)$|(^|/)schema/(full|player|match|hero|league|live|constants)\.graphql$|(^|/)constants/(heroes|items|abilities|game-modes|regions|ranks)\.json$'

violations="$(git ls-files | grep -E "$forbidden" || true)"
if [[ -n "$violations" ]]; then
  echo "restricted STRATZ artifacts are tracked:" >&2
  echo "$violations" >&2
  exit 1
fi

if git grep -l 'Generated from authenticated STRATZ introspection' -- '*.graphql' '*.json' 2>/dev/null; then
  echo "tracked files contain restricted authenticated STRATZ schema data" >&2
  exit 1
fi

if git grep -l '"source": "authenticated STRATZ GraphQL introspection"' -- '*.json' 2>/dev/null; then
  echo "tracked JSON contains restricted authenticated STRATZ schema data" >&2
  exit 1
fi
