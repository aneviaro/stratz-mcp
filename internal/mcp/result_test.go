package mcp

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func TestErrorResultPreservesGraphQLMessages(t *testing.T) {
	result, err := ErrorResult("stratz_execute_graphql", &ExecutionError{
		Code:      contracts.ErrorCodeUpstreamError,
		Message:   "STRATZ returned GraphQL execution errors",
		Retryable: false,
		Details: map[string]any{
			"graphql_messages": []string{"first diagnosis", "second diagnosis"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	compact, ok := result.StructuredContent.(json.RawMessage)
	if !ok {
		t.Fatalf("structured content type = %T", result.StructuredContent)
	}
	var structured map[string]any
	if err := json.Unmarshal(compact, &structured); err != nil {
		t.Fatal(err)
	}
	errorObject := structured["error"].(map[string]any)
	details := errorObject["details"].(map[string]any)
	if !reflect.DeepEqual(
		details["graphql_messages"],
		[]any{"first diagnosis", "second diagnosis"},
	) {
		t.Fatalf("graphql_messages = %#v", details["graphql_messages"])
	}
	if errorObject["message"] != "STRATZ returned GraphQL execution errors" {
		t.Fatalf("stable message = %#v", errorObject["message"])
	}
}
