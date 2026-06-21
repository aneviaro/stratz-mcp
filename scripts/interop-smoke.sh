#!/bin/sh
set -eu

mode=${1:-native}
target=${2:-dist/stratz-mcp}
client=${CLIENT_PROFILE:-codex}
input=$(mktemp)
output=$(mktemp)
errors=$(mktemp)
trap 'rm -f "$input" "$output" "$errors"' EXIT

cat > "$input" <<EOF
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"${client}-interop","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}
{"jsonrpc":"2.0","id":4,"method":"prompts/list","params":{}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"stratz_server_info","arguments":{}}}
EOF

case "$mode" in
	native)
		set +e
		{ cat "$input"; sleep 1; } |
			STRATZ_API_TOKEN=smoke-test-token "$target" serve > "$output" 2> "$errors"
		rc=$?
		set -e
		;;
	docker)
		set +e
		{ cat "$input"; sleep 1; } |
			docker run --rm -i --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m \
				-e STRATZ_API_TOKEN=smoke-test-token "$target" serve > "$output" 2> "$errors"
		rc=$?
		set -e
		;;
	*)
		echo "usage: $0 native <binary> | docker <image>" >&2
		exit 2
		;;
esac

if [ "${rc:-0}" -gt 1 ]; then
	cat "$errors" >&2
	exit "$rc"
fi

grep -q '"protocolVersion":"2025-11-25"' "$output"
grep -q '"name":"stratz_server_info"' "$output"
grep -q '"resources"' "$output"
grep -q '"prompts"' "$output"
grep -q '"cache_status":"healthy"' "$output"
if grep -q 'smoke-test-token' "$output" "$errors"; then
	echo "credential leaked during $client $mode smoke test" >&2
	exit 1
fi
printf '%s %s interoperability smoke passed\n' "$client" "$mode"
