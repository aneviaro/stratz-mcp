#!/bin/sh
set -eu

repo_root=${REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
repo_root=$(cd "$repo_root" && pwd)
go_cmd=${GO:-go}
mode=${1:-native}
target=${2:-dist/stratz-mcp}
client=${CLIENT_PROFILE:-codex}
protocol_profile=${MCP_PROTOCOL_PROFILE:-modern}
initialize_input=$(mktemp)
request_input=$(mktemp)
output=$(mktemp)
errors=$(mktemp)
validator=$(mktemp "$repo_root/interop-smoke.XXXXXX.go")
handshake_delay=${INTEROP_SMOKE_HANDSHAKE_DELAY:-2}
shutdown_delay=${INTEROP_SMOKE_SHUTDOWN_DELAY:-2}
trap 'rm -f "$initialize_input" "$request_input" "$output" "$errors" "$validator"' EXIT

case "$client" in
	codex|claude)
		;;
	*)
		echo "unknown CLIENT_PROFILE: $client" >&2
		exit 2
		;;
esac

modern_meta="\"_meta\":{\"io.modelcontextprotocol/protocolVersion\":\"2026-07-28\",\"io.modelcontextprotocol/clientInfo\":{\"name\":\"${client}-interop\",\"version\":\"1\"},\"io.modelcontextprotocol/clientCapabilities\":{}}"

case "$protocol_profile" in
	modern)
		cat > "$request_input" <<EOF
{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{${modern_meta}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{${modern_meta}}}
{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{${modern_meta}}}
{"jsonrpc":"2.0","id":4,"method":"prompts/list","params":{${modern_meta}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{${modern_meta},"name":"stratz_server_info","arguments":{}}}
EOF
		;;
	legacy)
		cat > "$initialize_input" <<EOF
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"${client}-interop","version":"1"}}}
EOF
		cat > "$request_input" <<EOF
{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}
{"jsonrpc":"2.0","id":4,"method":"prompts/list","params":{}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"stratz_server_info","arguments":{}}}
EOF
		;;
	*)
		echo "unknown MCP_PROTOCOL_PROFILE: $protocol_profile" >&2
		exit 2
		;;
esac

send_profile() {
	case "$protocol_profile" in
		modern)
			cat "$request_input"
			sleep "$shutdown_delay"
			;;
		legacy)
			cat "$initialize_input"
			sleep "$handshake_delay"
			cat "$request_input"
			sleep "$shutdown_delay"
			;;
	esac
}

case "$mode" in
	native)
		set +e
		send_profile | STRATZ_API_TOKEN=smoke-test-token "$target" serve > "$output" 2> "$errors"
		rc=$?
		set -e
		;;
	docker)
		set +e
		send_profile | docker run --rm -i --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m \
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

const (
	legacyProtocolVersion = "2025-11-25"
	catalogCacheTTL       = 300_000
	discoveryCacheTTL     = 3_600_000
)

var expectedV2Descriptions = map[string]string{
	"stratz_execute_graphql": "Execute one guarded GraphQL query against an approved STRATZ root.",
	"stratz_query_constants": "Query supported constants by types or typed selectors.",
	"stratz_query_heroes":    "Query normalized heroes exactly or by bounded search with optional statistics.",
	"stratz_query_leagues":   "Query normalized leagues exactly or by authenticated bounded search.",
	"stratz_query_matches":   "Query normalized matches by exact IDs, history filters, or live filters.",
	"stratz_query_players":   "Query normalized players by one to 25 exact identifiers.",
	"stratz_server_info":     "Return server, protocol, schema, cache, limits, and upstream status.",
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      implementation `json:"serverInfo"`
}

type cacheableResult struct {
	ResultType string         `json:"resultType"`
	Meta       map[string]any `json:"_meta"`
	TTLMs      int            `json:"ttlMs"`
	CacheScope string         `json:"cacheScope"`
}

type discoverResult struct {
	cacheableResult
	SupportedVersions []string       `json:"supportedVersions"`
	Capabilities      map[string]any `json:"capabilities"`
}

type listedTool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
}

type toolsListResult struct {
	cacheableResult
	Tools []listedTool `json:"tools"`
}

type listedResource struct {
	URI string `json:"uri"`
}

type resourcesListResult struct {
	cacheableResult
	Resources []listedResource `json:"resources"`
}

type listedPrompt struct {
	Name string `json:"name"`
}

type promptsListResult struct {
	cacheableResult
	Prompts []listedPrompt `json:"prompts"`
}

type callToolResult struct {
	cacheableResult
	StructuredContent map[string]any `json:"structuredContent"`
}

func main() {
	if len(os.Args) != 4 {
		fatalf("usage: interop-smoke-assert <jsonl-output> <protocol-profile> <client-profile>")
	}
	profile := os.Args[2]
	client := os.Args[3]
	if client != "codex" && client != "claude" {
		fatalf("unknown client profile %q", client)
	}

	results, err := loadResults(os.Args[1])
	if err != nil {
		fatalf("%v", err)
	}

	switch profile {
	case "modern":
		validateModern(results)
	case "legacy":
		validateLegacy(results)
	default:
		fatalf("unknown protocol profile %q", profile)
	}
}

func validateModern(results map[string]json.RawMessage) {
	var discover discoverResult
	decodeResult(results, "1", &discover)
	assertComplete("server/discover", discover.cacheableResult)
	assertCacheHints("server/discover", discover.TTLMs, discover.CacheScope, discoveryCacheTTL, "public")
	assertSupportedVersions("server/discover", discover.SupportedVersions)
	assertCapabilities("server/discover", discover.Capabilities)
	assertServerMeta("server/discover", discover.Meta)

	validateCatalogs(results)
	validateServerInfo(results, true)
}

func validateLegacy(results map[string]json.RawMessage) {
	var initialize initializeResult
	decodeResult(results, "1", &initialize)
	if initialize.ProtocolVersion != legacyProtocolVersion {
		fatalf("initialize protocol version = %q, want %q", initialize.ProtocolVersion, legacyProtocolVersion)
	}
	assertCapabilities("initialize", initialize.Capabilities)
	if initialize.ServerInfo.Name != "stratz-mcp" {
		fatalf("initialize server info = %#v, want stratz-mcp", initialize.ServerInfo)
	}

	validateCatalogs(results)
	validateServerInfo(results, false)
}

func validateCatalogs(results map[string]json.RawMessage) {
	var tools toolsListResult
	decodeResult(results, "2", &tools)
	assertProtocolResult("tools/list", tools.cacheableResult)
	assertCacheHints("tools/list", tools.TTLMs, tools.CacheScope, catalogCacheTTL, "public")
	if len(expectedV2Descriptions) != 7 || len(contracts.Definitions()) != 7 {
		fatalf("v2 catalog size = %d generated definitions/%d expected descriptions, want 7", len(contracts.Definitions()), len(expectedV2Descriptions))
	}
	wantToolNames := make([]string, 0, len(contracts.Definitions()))
	toolsByName := make(map[string]listedTool, len(tools.Tools))
	for _, definition := range contracts.Definitions() {
		wantToolNames = append(wantToolNames, definition.Name)
		expectedDescription, ok := expectedV2Descriptions[definition.Name]
		if !ok || definition.Description != expectedDescription {
			fatalf("v2 contract description for %s = %q, want %q", definition.Name, definition.Description, expectedDescription)
		}
	}
	gotToolNames := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		gotToolNames = append(gotToolNames, tool.Name)
		toolsByName[tool.Name] = tool
		expectedDescription, ok := expectedV2Descriptions[tool.Name]
		if !ok {
			fatalf("unexpected non-v2 tool %q in tools/list", tool.Name)
		}
		if tool.Description != expectedDescription {
			fatalf("%s description = %q, want %q", tool.Name, tool.Description, expectedDescription)
		}
		assertToolSchema(tool.Name, contracts.InputSchema, tool.InputSchema)
		assertToolSchema(tool.Name, contracts.OutputSchema, tool.OutputSchema)
	}
	assertExactStrings("v2 tool names", gotToolNames, append([]string(nil), wantToolNames...))
	for _, name := range wantToolNames {
		if _, ok := toolsByName[name]; !ok {
			fatalf("tool %s missing from tools/list", name)
		}
	}

	var resources resourcesListResult
	decodeResult(results, "3", &resources)
	assertProtocolResult("resources/list", resources.cacheableResult)
	assertCacheHints("resources/list", resources.TTLMs, resources.CacheScope, catalogCacheTTL, "public")
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
	decodeResult(results, "4", &prompts)
	assertProtocolResult("prompts/list", prompts.cacheableResult)
	assertCacheHints("prompts/list", prompts.TTLMs, prompts.CacheScope, catalogCacheTTL, "public")
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

func validateServerInfo(results map[string]json.RawMessage, modern bool) {
	var call callToolResult
	decodeResult(results, "5", &call)
	if modern {
		assertComplete("tools/call", call.cacheableResult)
		assertServerMeta("tools/call", call.Meta)
	} else if call.ResultType != "" {
		fatalf("legacy tools/call resultType = %q, want absent", call.ResultType)
	}
	data, ok := call.StructuredContent["data"].(map[string]any)
	if !ok {
		fatalf("server info structuredContent data = %#v", call.StructuredContent["data"])
	}
	if data["mcp_protocol_version"] != contracts.MCPProtocolVersion {
		fatalf("server info preferred version = %#v, want %s", data["mcp_protocol_version"], contracts.MCPProtocolVersion)
	}
	assertSupportedVersions("server info", data["supported_mcp_protocol_versions"])
	if data["cache_status"] != "healthy" {
		fatalf("server info cache_status = %#v, want healthy", data["cache_status"])
	}
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
		if len(message.Error) != 0 && string(message.Error) != "null" {
			return nil, fmt.Errorf("JSON-RPC error for id %s: %s", decodeID(message.ID), message.Error)
		}
		if len(message.Result) == 0 || string(message.Result) == "null" {
			continue
		}
		results[decodeID(message.ID)] = append([]byte(nil), message.Result...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan output: %w", err)
	}
	return results, nil
}

func decodeResult(results map[string]json.RawMessage, id string, target any) {
	result, ok := results[id]
	if !ok {
		fatalf("missing result for id %s", id)
	}
	if err := json.Unmarshal(result, target); err != nil {
		fatalf("decode result %s: %v", id, err)
	}
}

func assertProtocolResult(label string, result cacheableResult) {
	if result.ResultType == "complete" {
		assertServerMeta(label, result.Meta)
		return
	}
	if result.ResultType != "" {
		fatalf("%s resultType = %q, want complete or absent", label, result.ResultType)
	}
}

func assertComplete(label string, result cacheableResult) {
	if result.ResultType != "complete" {
		fatalf("%s resultType = %q, want complete", label, result.ResultType)
	}
}

func assertCacheHints(label string, gotTTL int, gotScope string, wantTTL int, wantScope string) {
	if gotTTL != wantTTL || gotScope != wantScope {
		fatalf("%s cache hints = %d/%q, want %d/%q", label, gotTTL, gotScope, wantTTL, wantScope)
	}
}

func assertServerMeta(label string, meta map[string]any) {
	serverInfo, ok := meta["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if !ok {
		fatalf("%s missing server info _meta: %#v", label, meta)
	}
	if serverInfo["name"] != "stratz-mcp" {
		fatalf("%s server info name = %#v, want stratz-mcp", label, serverInfo["name"])
	}
}

func assertCapabilities(label string, capabilities map[string]any) {
	for _, name := range []string{"tools", "resources", "prompts"} {
		capability, ok := capabilities[name].(map[string]any)
		if !ok {
			fatalf("%s missing %s capability: %#v", label, name, capabilities)
		}
		if len(capability) != 0 {
			fatalf("%s %s capability is dynamic: %#v", label, name, capability)
		}
	}
	for _, name := range []string{"roots", "sampling", "logging", "subscriptions"} {
		if _, ok := capabilities[name]; ok {
			fatalf("%s advertised unexpected capability %s: %#v", label, name, capabilities)
		}
	}
}

func assertSupportedVersions(label string, value any) {
	var got []string
	switch value := value.(type) {
	case []string:
		got = value
	case []any:
		got = make([]string, 0, len(value))
		for _, item := range value {
			version, ok := item.(string)
			if !ok {
				fatalf("%s supported version item = %#v, want string", label, item)
			}
			got = append(got, version)
		}
	default:
		fatalf("%s supported versions = %#v", label, value)
	}
	want := contracts.SupportedMCPProtocolVersions()
	if !slices.Equal(got, want) {
		fatalf("%s supported versions = %v, want %v", label, got, want)
	}
	if len(got) == 0 || got[0] != contracts.MCPProtocolVersion {
		fatalf("%s preferred version ordering = %v, want %s first", label, got, contracts.MCPProtocolVersion)
	}
}

func assertToolSchema(name string, kind contracts.SchemaKind, got any) {
	expectedJSON, err := contracts.Schema(name, kind)
	if err != nil {
		fatalf("load %s %s schema: %v", name, kind, err)
	}
	var expected any
	if err := json.Unmarshal(expectedJSON, &expected); err != nil {
		fatalf("decode %s %s schema: %v", name, kind, err)
	}
	expectedCompact, err := json.Marshal(expected)
	if err != nil {
		fatalf("marshal expected %s %s schema: %v", name, kind, err)
	}
	gotCompact, err := json.Marshal(got)
	if err != nil {
		fatalf("marshal got %s %s schema: %v", name, kind, err)
	}
	if !bytes.Equal(gotCompact, expectedCompact) {
		fatalf("%s %s schema differs from generated contract", name, kind)
	}
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

(cd "$repo_root" && "$go_cmd" run "$validator" "$output" "$protocol_profile" "$client")

if grep -q 'smoke-test-token' "$output" "$errors"; then
	echo "credential leaked during $client $protocol_profile $mode smoke test" >&2
	exit 1
fi
printf '%s %s %s interoperability smoke passed\n' "$client" "$protocol_profile" "$mode"
