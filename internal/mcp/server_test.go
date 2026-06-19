package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type serverExecutor struct{}

func (serverExecutor) Execute(
	context.Context,
	*stratz.RequestBudget,
	stratz.Request,
) (*stratz.Response, error) {
	limit := int64(150)
	remaining := int64(149)
	return &stratz.Response{
		RateLimits: []stratz.RateLimit{{
			Window:    "minute",
			Limit:     &limit,
			Remaining: &remaining,
			Source:    "fixture",
		}},
	}, nil
}

func testServer(t *testing.T, logger *slog.Logger) *Server {
	t.Helper()
	cfg := config.Defaults(t.TempDir())
	cfg.Cache.Enabled = false
	server, err := New(Options{
		Version:       "v1.2.3",
		SchemaVersion: "sha256:fixture",
		Config:        cfg,
		Executor:      serverExecutor{},
		Logger:        logger,
		Now: func() time.Time {
			return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
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
	for _, tool := range listed.Tools {
		assertToolSchema(t, tool.Name, contracts.InputSchema, tool.InputSchema)
		assertToolSchema(t, tool.Name, contracts.OutputSchema, tool.OutputSchema)
	}

	resources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Resources) != 0 {
		t.Fatalf("resources = %#v, want empty static list", resources.Resources)
	}
	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts.Prompts) != 0 {
		t.Fatalf("prompts = %#v, want empty static list", prompts.Prompts)
	}

	success, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_server_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_server_info", success, false)

	example, err := contracts.Example("stratz_get_player", contracts.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	executionFailure, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_get_player",
		Arguments: example,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_get_player", executionFailure, true)

	invalid, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_get_player",
		Arguments: map[string]any{"player_id": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResultConforms(t, "stratz_get_player", invalid, true)

	if _, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name:      "stratz_missing",
		Arguments: map[string]any{},
	}); err == nil {
		t.Fatal("unknown tool did not produce a protocol error")
	}
}

func TestRawStdioProtocolHarness(t *testing.T) {
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
	if initializeResult["protocolVersion"] != contracts.MCPProtocolVersion {
		t.Fatalf("initialize result = %#v", initializeResult)
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

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"stratz_server_info","arguments":{}}}`)
	success := readRaw(t, reader, &rawLines)
	successResult := success["result"].(map[string]any)
	if _, present := successResult["isError"]; present {
		t.Fatalf("success unexpectedly included isError: %#v", successResult)
	}
	assertRawMirror(t, successResult)

	writeRaw(t, clientWriter, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"stratz_get_player","arguments":{"player_id":"123"}}}`)
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

func assertResultConforms(
	t *testing.T,
	tool string,
	result *sdk.CallToolResult,
	wantError bool,
) {
	t.Helper()
	if result.IsError != wantError {
		t.Fatalf("isError = %v, want %v", result.IsError, wantError)
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
