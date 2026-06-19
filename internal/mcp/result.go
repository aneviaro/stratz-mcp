package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExecutionError is a stable tool-domain failure returned through MCP with
// isError=true.
type ExecutionError struct {
	Code        contracts.ErrorCode
	Message     string
	Retryable   bool
	RetryAfter  *string
	Details     map[string]any
	FailedInput any
	Context     any
}

func (err *ExecutionError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// SuccessResult validates and encodes one authoritative structured result.
func SuccessResult(tool string, output any) (*sdk.CallToolResult, error) {
	return encodeResult(tool, output, false)
}

// ErrorResult validates and encodes one stable execution-error envelope.
func ErrorResult(tool string, executionErr *ExecutionError) (*sdk.CallToolResult, error) {
	if executionErr == nil {
		return nil, errors.New("execution error is required")
	}
	details := executionErr.Details
	if details == nil {
		details = map[string]any{}
	}
	envelope := map[string]any{
		"kind": "error",
		"error": map[string]any{
			"code":        executionErr.Code,
			"message":     executionErr.Message,
			"retryable":   executionErr.Retryable,
			"retry_after": executionErr.RetryAfter,
			"details":     details,
		},
	}
	if executionErr.FailedInput != nil {
		envelope["error"].(map[string]any)["failed_input"] = executionErr.FailedInput
	}
	if executionErr.Context != nil {
		envelope["context"] = executionErr.Context
	}
	return encodeResult(tool, envelope, true)
}

func encodeResult(tool string, output any, isError bool) (*sdk.CallToolResult, error) {
	compact, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshal %s result: %w", tool, err)
	}
	decoded, err := decodeJSON(compact)
	if err != nil {
		return nil, fmt.Errorf("decode %s result for validation: %w", tool, err)
	}
	if err := contracts.ValidateOutput(tool, decoded); err != nil {
		return nil, err
	}
	return &sdk.CallToolResult{
		Content: []sdk.Content{
			&sdk.TextContent{Text: string(compact)},
		},
		StructuredContent: json.RawMessage(compact),
		IsError:           isError,
	}, nil
}

func decodeArguments(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return decodeJSON(raw)
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected trailing JSON value")
		}
		return nil, err
	}
	return value, nil
}
