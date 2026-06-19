#!/bin/sh
set -eu

go_cmd=${GO:-go}
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

{
	printf 'THIRD-PARTY NOTICES\n\n'
	printf 'Runtime module inventory generated from go.mod. License texts remain authoritative in each upstream module.\n\n'
	"$go_cmd" list -m -f '{{if not .Main}}{{.Path}} {{.Version}}{{end}}' all | LC_ALL=C sort
} > "$tmp"

if [ "${CHECK:-0}" = 1 ]; then
	cmp -s "$tmp" THIRD_PARTY_NOTICES || {
		echo "THIRD_PARTY_NOTICES is stale; run make notices" >&2
		exit 1
	}
else
	cp "$tmp" THIRD_PARTY_NOTICES
fi
