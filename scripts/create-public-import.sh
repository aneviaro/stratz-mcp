#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/create-public-import.sh [--allow-dirty] [--output-dir PATH]

Options:
  --allow-dirty      Skip the clean-worktree requirement.
  --output-dir PATH  Destination directory for the generated repository.
  -h, --help         Show this help text.

Environment:
  ALLOW_DIRTY  Truthy values (1, true, yes, on) skip the clean-worktree check.
  OUTPUT_DIR   Destination directory. Defaults to dist/public-import.
  MAKE         Make binary to invoke. Defaults to make.
EOF
}

is_truthy() {
  case "$1" in
    1|[Tt][Rr][Uu][Ee]|[Yy][Ee][Ss]|[Oo][Nn])
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
repo_root="$(cd "$repo_root" && pwd)"
make_cmd="${MAKE:-make}"
allow_dirty="${ALLOW_DIRTY:-0}"
output_dir="${OUTPUT_DIR:-dist/public-import}"
positional_output=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --allow-dirty)
      allow_dirty=1
      ;;
    --output-dir)
      shift
      if [[ $# -eq 0 ]]; then
        echo "missing value for --output-dir" >&2
        usage >&2
        exit 1
      fi
      output_dir="$1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
    *)
      if [[ -n "$positional_output" ]]; then
        echo "unexpected extra argument: $1" >&2
        usage >&2
        exit 1
      fi
      positional_output="$1"
      ;;
  esac
  shift
done

if [[ -n "$positional_output" ]]; then
  output_dir="$positional_output"
fi

if [[ "$output_dir" != /* ]]; then
  output_dir="$repo_root/$output_dir"
fi

output_parent="$(dirname "$output_dir")"
mkdir -p "$output_parent"
output_dir="$(cd "$output_parent" && pwd)/$(basename "$output_dir")"

if [[ "$output_dir" == "/" || "$output_dir" == "$repo_root" ]]; then
  echo "refusing to use unsafe output directory: $output_dir" >&2
  exit 1
fi

case "$output_dir" in
  "$repo_root/.git" | "$repo_root/.git/"*)
    echo "output directory must not be inside the source repository .git directory" >&2
    exit 1
    ;;
esac

if ! git -C "$repo_root" rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "public import must run inside a git repository" >&2
  exit 1
fi

relative_output_dir=""
case "$output_dir" in
  "$repo_root"/*)
    relative_output_dir="${output_dir#"$repo_root"/}"
    ;;
esac

path_is_under_output_dir() {
  local path="$1"
  if [[ -z "$relative_output_dir" ]]; then
    return 1
  fi
  case "$path" in
    "$relative_output_dir" | "$relative_output_dir"/*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

require_clean_worktree() {
  local line path old_path new_path
  local -a dirty_lines=()
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    path="${line:3}"
    if [[ "$path" == *" -> "* ]]; then
      old_path="${path%% -> *}"
      new_path="${path#* -> }"
      if path_is_under_output_dir "$old_path" && path_is_under_output_dir "$new_path"; then
        continue
      fi
    elif path_is_under_output_dir "$path"; then
      continue
    fi
    dirty_lines+=("$line")
  done < <(git -C "$repo_root" status --porcelain --untracked-files=all)

  if ((${#dirty_lines[@]} > 0)); then
    echo "public import requires a clean worktree; re-run with ALLOW_DIRTY=1 or --allow-dirty to override" >&2
    printf '%s\n' "${dirty_lines[@]}" >&2
    exit 1
  fi
}

denylisted_paths=(
  ".git"
  ".ralphex"
  "docs/plans"
  "docs/implementation-plan.md"
  ".env"
  "dist"
  ".bin"
  "cache.db"
  "cache.db-wal"
  "cache.db-shm"
  ".stratz-local"
  "introspection.json"
  "schema/full.graphql"
  "schema/player.graphql"
  "schema/match.graphql"
  "schema/hero.graphql"
  "schema/league.graphql"
  "schema/live.graphql"
  "schema/constants.graphql"
  "constants/heroes.json"
  "constants/items.json"
  "constants/abilities.json"
  "constants/game-modes.json"
  "constants/regions.json"
  "constants/ranks.json"
  ".stratz-restricted"
)

if ! is_truthy "$allow_dirty"; then
  require_clean_worktree
fi

cd "$repo_root"
"$make_cmd" public-readiness
"$make_cmd" verify-public-surface

scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/stratz-public-import.XXXXXX")"
trap 'rm -rf "$scratch_root"' EXIT
staging_dir="$scratch_root/export"
mkdir -p "$staging_dir"

git archive --format=tar HEAD | tar -xf - -C "$staging_dir"
for path in "${denylisted_paths[@]}"; do
  rm -rf "$staging_dir/$path"
done

rm -rf "$output_dir"
mkdir -p "$output_dir"
(cd "$staging_dir" && tar -cf - .) | (cd "$output_dir" && tar -xf -)

commit_name="$(git -C "$repo_root" config user.name || true)"
commit_email="$(git -C "$repo_root" config user.email || true)"
if [[ -z "$commit_name" ]]; then
  commit_name="Public Import"
fi
if [[ -z "$commit_email" ]]; then
  commit_email="public-import@example.invalid"
fi

cd "$output_dir"
git init >/dev/null
git add .
git -c user.name="$commit_name" -c user.email="$commit_email" commit -m "Initial public import" >/dev/null
git branch -M main
"$make_cmd" public-readiness
"$make_cmd" verify-public-surface
"$make_cmd" check

echo "Created clean public import at $output_dir"
