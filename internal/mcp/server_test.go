package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	promptcatalog "github.com/aneviaro/stratz-mcp/internal/prompts"
	resourcecatalog "github.com/aneviaro/stratz-mcp/internal/resources"
	"github.com/aneviaro/stratz-mcp/internal/schema"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type serverExecutor struct{}

func (serverExecutor) Execute(
	_ context.Context,
	_ *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	limit := int64(150)
	remaining := int64(149)
	data := json.RawMessage(`{"match":{"id":"1"}}`)
	if request.OperationName == "StratzGetPlayers" || request.OperationName == "StratzGetPlayersLean" {
		data = json.RawMessage(`{"players":[{"steamAccountId":1,"isPrivate":false}]}`)
	}
	return &stratz.Response{
		HTTPStatus: 200,
		Data:       data,
		Errors:     json.RawMessage(`[]`),
		Extensions: json.RawMessage(`null`),
		RateLimits: []stratz.RateLimit{{
			Window:    "minute",
			Limit:     &limit,
			Remaining: &remaining,
			Source:    "fixture",
		}},
	}, nil
}

type heroCursorExecutor struct{}

func (heroCursorExecutor) Execute(
	_ context.Context,
	_ *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	if request.OperationName != "StratzGetConstants" {
		return &stratz.Response{HTTPStatus: 200, Data: json.RawMessage(`{"match":{"id":"1"}}`)}, nil
	}
	return &stratz.Response{HTTPStatus: 200, Data: json.RawMessage(`{"constants":{"heroes":[
		{"id":1,"name":"npc_dota_hero_axe","localizedName":"Axe","primaryAttribute":"strength","attackType":"melee","roles":[]},
		{"id":2,"name":"npc_dota_hero_bane","localizedName":"Bane","primaryAttribute":"intelligence","attackType":"ranged","roles":[]}
	]}}`)}, nil
}

func testServer(t *testing.T, logger *slog.Logger, handlers ...map[string]ToolHandler) *Server {
	t.Helper()
	return testServerWithExecutor(t, logger, serverExecutor{}, "fixture-token", "sha256:fixture", handlers...)
}

func testServerWithExecutor(
	t *testing.T,
	logger *slog.Logger,
	executor stratz.Executor,
	cursorToken string,
	schemaVersion string,
	handlers ...map[string]ToolHandler,
) *Server {
	t.Helper()
	cfg := config.Defaults(t.TempDir())
	cfg.Cache.Enabled = false
	schemaDirectory := t.TempDir()
	for _, definition := range resourcecatalog.Definitions() {
		path := filepath.Join(schemaDirectory, filepath.FromSlash(definition.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		content := []byte("{}\n")
		if definition.MIMEType == "application/graphql" {
			content = []byte("scalar Long\ntype Query { match(id: Long!): Match }\ntype Match { id: Long! }\n")
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := schema.Manifest{
		FormatVersion: schema.FormatVersion,
		SchemaHash:    "sha256:fixture",
		Validation: schema.ValidationMetadata{
			QueryType: "Query",
			Fields: map[string]map[string]schema.Ref{
				"Query": {
					"match": {
						Type: "Match",
						Arguments: map[string]schema.Ref{
							"id": {Type: "Long!"},
						},
					},
				},
				"Match": {"id": {Type: "Long!"}},
			},
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(schemaDirectory, schema.ManifestFile),
		manifestData,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var configuredHandlers map[string]ToolHandler
	if len(handlers) > 0 {
		configuredHandlers = handlers[0]
	}
	server, err := New(Options{
		Version:         "v1.2.3",
		SchemaVersion:   schemaVersion,
		SchemaDirectory: schemaDirectory,
		Config:          cfg,
		Executor:        executor,
		CursorToken:     cursorToken,
		Logger:          logger,
		Handlers:        configuredHandlers,
		Now: func() time.Time {
			return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func callHeroSearch(t *testing.T, server *Server, arguments map[string]any) *sdk.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(
		&sdk.Implementation{Name: "hero-cursor-test", Version: "1"},
		&sdk.ClientOptions{},
	)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name: "stratz_query_heroes", Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHeroSearchCursorUsesProductionServerBinding(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	firstServer := testServerWithExecutor(t, logger, heroCursorExecutor{}, "first-token", "schema-one")
	first := callHeroSearch(t, firstServer, map[string]any{
		"mode": "search", "query": "hero", "limit": 1,
	})
	if first.IsError {
		t.Fatalf("first hero search failed: %#v", first.StructuredContent)
	}
	envelope, ok := first.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("hero search envelope = %T", first.StructuredContent)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("hero search data = %#v", envelope["data"])
	}
	page, ok := data["page"].(map[string]any)
	if !ok {
		t.Fatalf("hero search page = %#v", data["page"])
	}
	cursor, ok := page["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("hero search cursor = %#v", page["next_cursor"])
	}

	schemaServer := testServerWithExecutor(t, logger, heroCursorExecutor{}, "first-token", "schema-two")
	schemaResult := callHeroSearch(t, schemaServer, map[string]any{
		"mode": "search", "query": "hero", "limit": 1, "cursor": cursor,
	})
	if !schemaResult.IsError {
		t.Fatalf("hero cursor accepted a different production schema: %#v", schemaResult.StructuredContent)
	}
	secondServer := testServerWithExecutor(t, logger, heroCursorExecutor{}, "second-token", "schema-one")
	second := callHeroSearch(t, secondServer, map[string]any{
		"mode": "search", "query": "hero", "limit": 1, "cursor": cursor,
	})
	if !second.IsError {
		t.Fatalf("hero cursor accepted a different production token: %#v", second.StructuredContent)
	}
}

func TestPublicRateLimitsDedupeAndCap(t *testing.T) {
	firstRemaining := int64(149)
	latestRemaining := int64(145)
	minuteLimit := int64(150)
	rateLimits := []stratz.RateLimit{
		{Window: "minute", Limit: &minuteLimit, Remaining: &firstRemaining, Source: "fixture-minute"},
		{Window: "minute", Limit: &minuteLimit, Remaining: &latestRemaining, Source: "fixture-minute"},
	}
	for index := range 10 {
		limit := int64(100 + index)
		remaining := int64(90 + index)
		rateLimits = append(rateLimits, stratz.RateLimit{
			Window:    "unknown",
			Limit:     &limit,
			Remaining: &remaining,
			Source:    fmt.Sprintf("extra-%d", index),
		})
	}

	public := publicRateLimits(rateLimits)
	if len(public) != maxPublicRateLimits {
		t.Fatalf("rate limit count = %d, want %d", len(public), maxPublicRateLimits)
	}
	first := public[0].(map[string]any)
	if first["window"] != "minute" || first["remaining"] != &latestRemaining {
		t.Fatalf("first rate limit = %#v, want latest minute fixture", first)
	}
}

func TestSDKConformance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := testServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)))
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := sdk.NewClient(
		&sdk.Implementation{Name: "stratz-mcp-test", Version: "1"},
		&sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}},
	)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	initialize := clientSession.InitializeResult()
	if initialize.ProtocolVersion != contracts.MCPProtocolVersion {
		t.Fatalf(
			"protocol version = %q, want %q",
			initialize.ProtocolVersion,
			contracts.MCPProtocolVersion,
		)
	}
	assertStaticCapabilities(t, initialize.Capabilities)

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != len(contracts.Definitions()) {
		t.Fatalf("tool count = %d, want %d", len(listed.Tools), len(contracts.Definitions()))
	}
	assertListedToolNames(t, listed.Tools)
	for _, tool := range listed.Tools {
		if len([]byte(tool.Description)) > 96 {
			t.Fatalf("%s description is %d bytes, want <= 96", tool.Name, len([]byte(tool.Description)))
		}
		assertToolSchemaDraft(t, tool.Name, contracts.InputSchema, tool.InputSchema)
		assertToolSchemaDraft(t, tool.Name, contracts.OutputSchema, tool.OutputSchema)
		assertToolSchema(t, tool.Name, contracts.InputSchema, tool.InputSchema)
		assertToolSchema(t, tool.Name, contracts.OutputSchema, tool.OutputSchema)
	}

	listedResources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listedResources.Resources) != len(resourcecatalog.Definitions()) {
		t.Fatalf(
			"resource count = %d, want %d",
			len(listedResources.Resources),
			len(resourcecatalog.Definitions()),
		)
	}
	assertListedResourceURIs(t, listedResources.Resources)
	readResource, err := clientSession.ReadResource(ctx, &sdk.ReadResourceParams{
		URI: "stratz://schema/full",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(readResource.Contents) != 1 ||
		readResource.Contents[0].Text != "scalar Long\ntype Query { match(id: Long!): Match }\ntype Match { id: Long! }\n" {
		t.Fatalf("read resource = %#v", readResource)
	}
	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts.Prompts) != len(promptcatalog.Definitions()) {
		t.Fatalf("prompt count = %d, want %d", len(prompts.Prompts), len(promptcatalog.Definitions()))
	}
	assertListedPromptNames(t, prompts.Prompts)
	matchPrompt, err := clientSession.GetPrompt(ctx, &sdk.GetPromptParams{
		Name:      "analyze_dota_match",
		Arguments: map[string]string{"match_id": "123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(matchPrompt.Messages) != 1 {
		t.Fatalf("prompt messages = %d, want 1", len(matchPrompt.Messages))
	}
	promptText, ok := matchPrompt.Messages[0].Content.(*sdk.TextContent)
	if !ok || !strings.Contains(promptText.Text, "match_id: \"123\"") {
		t.Fatalf("unexpected prompt content %#v", matchPrompt.Messages[0].Content)
	}

	success, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_server_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_server_info", success, false)
	serverInfoOutput, ok := success.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("server info structured content = %T", success.StructuredContent)
	}
	serverInfoData, ok := serverInfoOutput["data"].(map[string]any)
	if !ok || serverInfoData["mcp_protocol_version"] != contracts.MCPProtocolVersion {
		t.Fatalf("server info data = %#v", serverInfoOutput["data"])
	}
	if got, ok := serverInfoData["supported_mcp_protocol_versions"].([]any); !ok || len(got) != len(contracts.SupportedMCPProtocolVersions()) {
		t.Fatalf("server info supported versions = %#v", serverInfoData["supported_mcp_protocol_versions"])
	}

	rawSuccess, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name: "stratz_execute_graphql",
		Arguments: map[string]any{
			"query": `query Match { match(id: 1) { id } }`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_execute_graphql", rawSuccess, false)

	rawPolicyFailure, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name: "stratz_execute_graphql",
		Arguments: map[string]any{
			"query": `mutation { match(id: 1) { id } }`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_execute_graphql", rawPolicyFailure, true)

	rawCacheFailure, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name: "stratz_execute_graphql",
		Arguments: map[string]any{
			"query": `query { match(id: 1) { id } }`,
			"cache": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_execute_graphql", rawCacheFailure, true)

	curatedSuccess, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_query_players",
		Arguments: map[string]any{"mode": "exact", "player_ids": []any{"1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_query_players", curatedSuccess, false)

	invalid, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_query_players",
		Arguments: map[string]any{"mode": "exact", "player_ids": []any{""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_query_players", invalid, true)

	if _, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_missing",
		Arguments: map[string]any{},
	}); err == nil {
		t.Fatal("unknown tool did not produce a protocol error")
	}
}

func TestEveryToolRunsThroughTheRegisteredAdapter(t *testing.T) {
	handlers := map[string]ToolHandler{}
	for _, definition := range contracts.Definitions() {
		if definition.Name == "stratz_server_info" {
			continue
		}
		output, err := contracts.Example(definition.Name, contracts.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		fixture := output
		handlers[definition.Name] = func(context.Context, any) (any, error) {
			return fixture, nil
		}
	}
	cfg := config.Defaults(t.TempDir())
	cfg.Cache.Enabled = false
	server, err := New(Options{
		Version:       "test",
		SchemaVersion: "schema-v1",
		Config:        cfg,
		Executor:      serverExecutor{},
		Handlers:      handlers,
		Now: func() time.Time {
			return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(
		&sdk.Implementation{Name: "adapter-test", Version: "1"},
		&sdk.ClientOptions{},
	)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	for _, definition := range contracts.Definitions() {
		t.Run(definition.Name, func(t *testing.T) {
			input, err := contracts.Example(definition.Name, contracts.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
				Name:      definition.Name,
				Arguments: input,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertResultConforms(t, definition.Name, result, false)
		})
	}
}

func TestRawModernStdioProtocolHarness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var handlerCalls atomic.Int64
	server := testServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)), map[string]ToolHandler{
		"stratz_query_players": func(context.Context, any) (any, error) {
			handlerCalls.Add(1)
			return nil, fmt.Errorf("unexpected handler invocation")
		},
	})
	serverInput, clientWriter := io.Pipe()
	clientReader, serverOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverInput, serverOutput)
	}()
	reader := bufio.NewReader(clientReader)
	var rawLines [][]byte
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"raw-modern","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+meta+`}}`)
	list := readRaw(t, reader, &rawLines)
	listResult, ok := list["result"].(map[string]any)
	if !ok || listResult["resultType"] != "complete" || listResult["ttlMs"] != float64(protocolCatalogCacheTTL) || listResult["cacheScope"] != "public" {
		t.Fatalf("modern tools/list result = %#v", list)
	}
	assertRawToolCatalog(t, listResult["tools"])

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"stratz_query_players","arguments":{"mode":"exact","player_ids":["1"]}}}`)
	missingMetadata := readRaw(t, reader, &rawLines)
	missingMetadataError, ok := missingMetadata["error"].(map[string]any)
	if !ok || missingMetadataError["code"] != float64(-32602) || missingMetadata["result"] != nil {
		t.Fatalf("metadata-free modern request = %#v, want -32602 and no result", missingMetadata)
	}
	if calls := handlerCalls.Load(); calls != 0 {
		t.Fatalf("metadata-free modern request invoked handler %d times, want 0", calls)
	}

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":3,"method":"server/discover","params":{`+meta+`}}`)
	discover := readRaw(t, reader, &rawLines)
	discoverResult, ok := discover["result"].(map[string]any)
	if !ok || discoverResult["resultType"] != "complete" || discoverResult["ttlMs"] != float64(protocolDiscoveryCacheTTL) || discoverResult["cacheScope"] != "public" {
		t.Fatalf("modern server/discover result = %#v", discover)
	}
	assertRawSupportedVersions(t, discoverResult["supportedVersions"])
	serverInfo, ok := discoverResult["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != serverName || serverInfo["version"] != "v1.2.3" {
		t.Fatalf("server identity metadata = %#v", discoverResult["_meta"])
	}
	capabilities, ok := discoverResult["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("discover capabilities = %#v", discoverResult["capabilities"])
	}
	for _, name := range []string{"tools", "resources", "prompts"} {
		capability, ok := capabilities[name].(map[string]any)
		if !ok {
			t.Fatalf("discover missing %s capability: %#v", name, capabilities)
		}
		if len(capability) != 0 {
			t.Fatalf("discover %s capability is dynamic: %#v", name, capability)
		}
	}
	for _, name := range []string{"roots", "sampling", "logging", "subscriptions"} {
		if _, ok := capabilities[name]; ok {
			t.Fatalf("discover advertised deprecated capability %s: %#v", name, capabilities)
		}
	}

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{`+meta+`,"name":"stratz_server_info","arguments":{}}}`)
	info := readRaw(t, reader, &rawLines)
	infoResult := info["result"].(map[string]any)
	assertRawMirror(t, infoResult)
	infoData := infoResult["structuredContent"].(map[string]any)["data"].(map[string]any)
	if infoData["mcp_protocol_version"] != contracts.MCPProtocolVersion {
		t.Fatalf("server info preferred version = %#v", infoData["mcp_protocol_version"])
	}
	assertRawSupportedVersions(t, infoData["supported_mcp_protocol_versions"])

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{`+meta+`,"uri":"stratz://schema/full"}}`)
	readResource := readRaw(t, reader, &rawLines)
	readResult, ok := readResource["result"].(map[string]any)
	if !ok || readResult["ttlMs"] != float64(0) || readResult["cacheScope"] != "private" {
		t.Fatalf("resource read cache hints = %#v, want 0/private", readResource)
	}
	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{`+meta+`,"uri":"stratz://schema/missing"}}`)
	missingResource := readRaw(t, reader, &rawLines)
	missingError, ok := missingResource["error"].(map[string]any)
	if !ok || missingError["code"] != float64(-32602) {
		t.Fatalf("missing resource error = %#v, want -32602", missingResource)
	}

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2099-01-01","io.modelcontextprotocol/clientInfo":{"name":"raw-modern","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}},"name":"stratz_server_info","arguments":{}}}`)
	unsupported := readRaw(t, reader, &rawLines)
	if unsupported["error"].(map[string]any)["code"] != float64(-32022) {
		t.Fatalf("unsupported version error = %#v, want -32022", unsupported)
	}

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25"},"name":"stratz_query_players","arguments":{"mode":"exact","player_ids":["1"]}}}`)
	downgrade := readRaw(t, reader, &rawLines)
	downgradeError, ok := downgrade["error"].(map[string]any)
	if !ok || downgradeError["code"] != float64(-32602) || downgrade["result"] != nil {
		t.Fatalf("modern protocol downgrade = %#v, want -32602 and no result", downgrade)
	}
	if calls := handlerCalls.Load(); calls != 0 {
		t.Fatalf("modern protocol downgrade invoked handler %d times, want 0", calls)
	}

	closeRawHarness(t, ctx, clientWriter, clientReader, serverDone, rawLines)
}

func TestRawLegacyStdioProtocolHarness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var diagnostics bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&diagnostics, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	server := testServer(t, logger)

	serverInput, clientWriter := io.Pipe()
	clientReader, serverOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverInput, serverOutput)
	}()
	reader := bufio.NewReader(clientReader)
	var rawLines [][]byte

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	preInitialize := readRaw(t, reader, &rawLines)
	if preInitialize["error"] == nil {
		t.Fatalf("pre-initialize call was accepted: %#v", preInitialize)
	}

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw-test","version":"1"}}}`)
	initialize := readRaw(t, reader, &rawLines)
	initializeResult := initialize["result"].(map[string]any)
	if initializeResult["protocolVersion"] != legacyProtocolVersion {
		t.Fatalf("initialize protocol version = %#v, want %s", initializeResult["protocolVersion"], legacyProtocolVersion)
	}
	capabilities := initializeResult["capabilities"].(map[string]any)
	for _, name := range []string{"tools", "resources", "prompts"} {
		capability, ok := capabilities[name].(map[string]any)
		if !ok {
			t.Fatalf("missing %s capability: %#v", name, capabilities)
		}
		if len(capability) != 0 {
			t.Fatalf("%s capability is dynamic: %#v", name, capability)
		}
	}
	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":10,"method":"tools/list","params":{}}`)
	legacyTools := readRaw(t, reader, &rawLines)
	legacyToolsResult, ok := legacyTools["result"].(map[string]any)
	if !ok {
		t.Fatalf("legacy tools/list result = %#v", legacyTools)
	}
	assertRawToolCatalog(t, legacyToolsResult["tools"])

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"stratz_server_info","arguments":{}}}`)
	success := readRaw(t, reader, &rawLines)
	successResult := success["result"].(map[string]any)
	if _, present := successResult["isError"]; present {
		t.Fatalf("success unexpectedly included isError: %#v", successResult)
	}
	if _, present := successResult["resultType"]; present {
		t.Fatalf("legacy success unexpectedly included resultType: %#v", successResult)
	}
	assertRawMirror(t, successResult)

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"stratz_query_players","arguments":{"mode":"exact","player_ids":["abc"]}}}`)
	executionFailure := readRaw(t, reader, &rawLines)
	failureResult := executionFailure["result"].(map[string]any)
	if failureResult["isError"] != true {
		t.Fatalf("known-tool failure was not an execution error: %#v", failureResult)
	}
	assertRawMirror(t, failureResult)

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"stratz_missing","arguments":{}}}`)
	protocolFailure := readRaw(t, reader, &rawLines)
	if protocolFailure["error"] == nil || protocolFailure["result"] != nil {
		t.Fatalf("unknown tool did not use JSON-RPC error path: %#v", protocolFailure)
	}

	closeRawHarness(t, ctx, clientWriter, clientReader, serverDone, rawLines)
	if !strings.Contains(diagnostics.String(), "initialization") {
		t.Fatalf("expected lifecycle diagnostic on stderr, got %q", diagnostics.String())
	}
	for _, line := range rawLines {
		if bytes.Contains(line, []byte("level=ERROR")) ||
			bytes.Contains(line, []byte("msg=")) {
			t.Fatalf("stderr diagnostic leaked into stdout: %q", line)
		}
	}
}

func TestResultEncoderRejectsSchemaInvalidOutput(t *testing.T) {
	if _, err := SuccessResult("stratz_server_info", map[string]any{
		"kind": "success",
	}); err == nil {
		t.Fatal("SuccessResult accepted schema-invalid output")
	}
}

func assertStaticCapabilities(t *testing.T, capabilities *sdk.ServerCapabilities) {
	t.Helper()
	if capabilities == nil ||
		capabilities.Tools == nil ||
		capabilities.Resources == nil ||
		capabilities.Prompts == nil {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if capabilities.Tools.ListChanged ||
		capabilities.Resources.ListChanged ||
		capabilities.Resources.Subscribe ||
		capabilities.Prompts.ListChanged {
		t.Fatalf("capabilities are not static: %#v", capabilities)
	}
	if capabilities.Logging != nil {
		t.Fatalf("unexpected protocol logging capability: %#v", capabilities.Logging)
	}
}

func assertToolSchema(t *testing.T, name string, kind contracts.SchemaKind, got any) {
	t.Helper()
	expectedJSON, err := contracts.Schema(name, kind)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := decodeJSON(expectedJSON)
	if err != nil {
		t.Fatal(err)
	}
	expectedCompact, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	gotCompact, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotCompact, expectedCompact) {
		t.Fatalf("%s %s schema differs from generated contract", name, kind)
	}
}

func assertToolSchemaDraft(t *testing.T, name string, kind contracts.SchemaKind, got any) {
	t.Helper()
	schema, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("%s %s schema type = %T, want JSON object", name, kind, got)
	}
	if schema["$schema"] != contracts.SchemaDraft {
		t.Fatalf("%s %s schema draft = %v, want %s", name, kind, schema["$schema"], contracts.SchemaDraft)
	}
}

func assertListedToolNames(t *testing.T, tools []*sdk.Tool) {
	t.Helper()
	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.Name)
	}
	want := make([]string, 0, len(contracts.Definitions()))
	for _, definition := range contracts.Definitions() {
		want = append(want, definition.Name)
	}
	assertExactStrings(t, "tool names", got, want)
}

func assertListedResourceURIs(t *testing.T, resources []*sdk.Resource) {
	t.Helper()
	got := make([]string, 0, len(resources))
	for _, resource := range resources {
		got = append(got, resource.URI)
	}
	want := make([]string, 0, len(resourcecatalog.Definitions()))
	for _, definition := range resourcecatalog.Definitions() {
		want = append(want, definition.URI)
	}
	assertExactStrings(t, "resource URIs", got, want)
}

func assertListedPromptNames(t *testing.T, prompts []*sdk.Prompt) {
	t.Helper()
	got := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		got = append(got, prompt.Name)
	}
	want := make([]string, 0, len(promptcatalog.Definitions()))
	for _, definition := range promptcatalog.Definitions() {
		want = append(want, definition.Name)
	}
	assertExactStrings(t, "prompt names", got, want)
}

func assertExactStrings(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func assertResultConforms(
	t *testing.T,
	tool string,
	result *sdk.CallToolResult,
	wantError bool,
) {
	t.Helper()
	if result.IsError != wantError {
		t.Fatalf("isError = %v, want %v; structured=%#v", result.IsError, wantError, result.StructuredContent)
	}
	if err := contracts.ValidateOutput(tool, result.StructuredContent); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want text", result.Content[0])
	}
	compact, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if text.Text != string(compact) {
		t.Fatalf("text mirror = %q, structured = %s", text.Text, compact)
	}
}

func closeRawHarness(
	t *testing.T,
	ctx context.Context,
	clientWriter io.Closer,
	clientReader io.Closer,
	serverDone <-chan error,
	rawLines [][]byte,
) {
	t.Helper()
	if err := clientWriter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("server shutdown failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("server did not shut down after stdin closed")
	}
	_ = clientReader.Close()
	for _, line := range rawLines {
		if !json.Valid(line) {
			t.Fatalf("stdout contained non-JSON protocol data: %q", line)
		}
	}
}

func assertRawToolCatalog(t *testing.T, value any) {
	t.Helper()
	tools, ok := value.([]any)
	if !ok || len(tools) != len(contracts.Definitions()) {
		t.Fatalf("raw tool catalog = %#v", value)
	}
	got := make([]string, 0, len(tools))
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("raw tool = %#v", rawTool)
		}
		name, ok := tool["name"].(string)
		if !ok {
			t.Fatalf("raw tool name = %#v", tool["name"])
		}
		if description, ok := tool["description"].(string); !ok || len([]byte(description)) > 96 {
			t.Fatalf("raw tool description = %#v", tool["description"])
		}
		got = append(got, name)
	}
	want := make([]string, 0, len(contracts.Definitions()))
	for _, definition := range contracts.Definitions() {
		want = append(want, definition.Name)
	}
	assertExactStrings(t, "raw tool names", got, want)
}

func assertRawSupportedVersions(t *testing.T, value any) {
	t.Helper()
	got, ok := value.([]any)
	if !ok || len(got) != len(contracts.SupportedMCPProtocolVersions()) {
		t.Fatalf("supported versions = %#v", value)
	}
	for index, version := range contracts.SupportedMCPProtocolVersions() {
		if got[index] != version {
			t.Fatalf("supported version %d = %v, want %s", index, got[index], version)
		}
	}
}

func writeRaw(t *testing.T, writer io.Writer, message string) {
	t.Helper()
	if _, err := fmt.Fprintln(writer, message); err != nil {
		t.Fatal(err)
	}
}

func readRaw(
	t *testing.T,
	reader *bufio.Reader,
	lines *[][]byte,
) map[string]any {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	line = bytes.TrimSpace(line)
	*lines = append(*lines, append([]byte(nil), line...))
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		t.Fatalf("decode raw response %q: %v", line, err)
	}
	return message
}

func assertRawMirror(t *testing.T, result map[string]any) {
	t.Helper()
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	compact, err := json.Marshal(result["structuredContent"])
	if err != nil {
		t.Fatal(err)
	}
	if text != string(compact) {
		t.Fatalf("text mirror = %q, structured = %s", text, compact)
	}
}
