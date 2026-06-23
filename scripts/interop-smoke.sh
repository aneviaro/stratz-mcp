#!/bin/sh
set -eu

repo_root=${REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
repo_root=$(cd "$repo_root" && pwd)
go_cmd=${GO:-go}
mode=${1:-native}
target=${2:-dist/stratz-mcp}
client=${CLIENT_PROFILE:-codex}
input=$(mktemp)
output=$(mktemp)
errors=$(mktemp)
validator=$(mktemp "$repo_root/interop-smoke.XXXXXX.go")
trap 'rm -f "$input" "$output" "$errors" "$validator"' EXIT

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

cat > "$validator" <<'EOF'
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	promptcatalog "github.com/aneviaro/stratz-mcp/internal/prompts"
	resourcecatalog "github.com/aneviaro/stratz-mcp/internal/resources"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
}

type listedTool struct {
	Name         string         `json:"name"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
}

type toolsListResult struct {
	Tools []listedTool `json:"tools"`
}

type listedResource struct {
	URI string `json:"uri"`
}

type resourcesListResult struct {
	Resources []listedResource `json:"resources"`
}

type listedPrompt struct {
	Name string `json:"name"`
}

type promptsListResult struct {
	Prompts []listedPrompt `json:"prompts"`
}

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: interop-smoke-assert <jsonl-output>")
	}

	results, err := loadResults(os.Args[1])
	if err != nil {
		fatalf("%v", err)
	}

	var tools toolsListResult
	if err := decodeResult(results, "2", &tools); err != nil {
		fatalf("decode tools/list result: %v", err)
	}
	wantToolNames := make([]string, 0, len(contracts.Definitions()))
	for _, definition := range contracts.Definitions() {
		wantToolNames = append(wantToolNames, definition.Name)
	}
	gotToolNames := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		gotToolNames = append(gotToolNames, tool.Name)
		if tool.InputSchema["$schema"] != contracts.SchemaDraft {
			fatalf("%s input schema draft = %v, want %s", tool.Name, tool.InputSchema["$schema"], contracts.SchemaDraft)
		}
		if tool.OutputSchema["$schema"] != contracts.SchemaDraft {
			fatalf("%s output schema draft = %v, want %s", tool.Name, tool.OutputSchema["$schema"], contracts.SchemaDraft)
		}
	}
	assertExactStrings("tool names", gotToolNames, wantToolNames)

	var resources resourcesListResult
	if err := decodeResult(results, "3", &resources); err != nil {
		fatalf("decode resources/list result: %v", err)
	}
	wantResourceURIs := make([]string, 0, len(resourcecatalog.Definitions()))
	for _, definition := range resourcecatalog.Definitions() {
		wantResourceURIs = append(wantResourceURIs, definition.URI)
	}
	gotResourceURIs := make([]string, 0, len(resources.Resources))
	for _, resource := range resources.Resources {
		gotResourceURIs = append(gotResourceURIs, resource.URI)
	}
	assertExactStrings("resource URIs", gotResourceURIs, wantResourceURIs)

	var prompts promptsListResult
	if err := decodeResult(results, "4", &prompts); err != nil {
		fatalf("decode prompts/list result: %v", err)
	}
	wantPromptNames := make([]string, 0, len(promptcatalog.Definitions()))
	for _, definition := range promptcatalog.Definitions() {
		wantPromptNames = append(wantPromptNames, definition.Name)
	}
	gotPromptNames := make([]string, 0, len(prompts.Prompts))
	for _, prompt := range prompts.Prompts {
		gotPromptNames = append(gotPromptNames, prompt.Name)
	}
	assertExactStrings("prompt names", gotPromptNames, wantPromptNames)
}

func loadResults(path string) (map[string]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open output: %w", err)
	}
	defer file.Close()

	results := map[string]json.RawMessage{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, fmt.Errorf("decode JSON-RPC line %q: %w", line, err)
		}
		if len(message.Result) == 0 {
			continue
		}
		results[decodeID(message.ID)] = append([]byte(nil), message.Result...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan output: %w", err)
	}
	return results, nil
}

func decodeResult(results map[string]json.RawMessage, id string, target any) error {
	result, ok := results[id]
	if !ok {
		return fmt.Errorf("missing result for id %s", id)
	}
	if err := json.Unmarshal(result, target); err != nil {
		return err
	}
	return nil
}

func decodeID(raw json.RawMessage) string {
	var integer int
	if err := json.Unmarshal(raw, &integer); err == nil {
		return strconv.Itoa(integer)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func assertExactStrings(label string, got []string, want []string) {
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		fatalf("%s mismatch: got %v want %v", label, got, want)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
EOF

(cd "$repo_root" && "$go_cmd" run "$validator" "$output")

grep -q '"cache_status":"healthy"' "$output"
if grep -q 'smoke-test-token' "$output" "$errors"; then
	echo "credential leaked during $client $mode smoke test" >&2
	exit 1
fi
printf '%s %s interoperability smoke passed\n' "$client" "$mode"
