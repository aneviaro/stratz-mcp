package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/graphql/policy"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

type recordingExecutor struct {
	request  stratz.Request
	budget   *stratz.RequestBudget
	response *stratz.Response
	err      error
	calls    int
}

func (executor *recordingExecutor) Execute(
	_ context.Context,
	budget *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	executor.calls++
	executor.request = request
	executor.budget = budget
	return executor.response, executor.err
}

func TestRawServiceExecutesCanonicalRequestAndPreservesPartialResponse(t *testing.T) {
	executor := &recordingExecutor{
		response: &stratz.Response{
			HTTPStatus: httpStatusOK,
			Data:       json.RawMessage(`{"match":{"id":"1"}}`),
			Errors: json.RawMessage(`[
				{"message":"partial","path":["match","players"]}
			]`),
			Extensions: json.RawMessage(`{"trace":"safe"}`),
			Partial:    true,
		},
	}
	service := mustRawService(t, executor, nil)
	result, err := service.Execute(context.Background(), RawRequest{
		Query:         `query Match($id: Long!) { match(id:$id){ id } }`,
		Variables:     map[string]any{"id": "1"},
		OperationName: "Match",
	})
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.request.Mode != stratz.ModeRaw ||
		!executor.request.AllowRetries {
		t.Fatalf("request = %#v, calls = %d", executor.request, executor.calls)
	}
	if executor.request.OperationName != "Match" {
		t.Fatalf("operation = %q", executor.request.OperationName)
	}
	if executor.budget.Remaining() != 5 {
		t.Fatalf("budget remaining before test executor consumption = %d, want 5", executor.budget.Remaining())
	}
	if result.HTTPStatus != httpStatusOK || !result.Partial ||
		len(result.Errors) != 1 {
		t.Fatalf("result = %#v", result)
	}
	data := result.Data.(map[string]any)
	if data["match"].(map[string]any)["id"] != "1" {
		t.Fatalf("data = %#v", result.Data)
	}
	if result.Extensions.(map[string]any)["trace"] != "safe" {
		t.Fatalf("extensions = %#v", result.Extensions)
	}
}

func TestRawServiceMarksDataAndErrorsPartial(t *testing.T) {
	executor := &recordingExecutor{
		response: &stratz.Response{
			HTTPStatus: httpStatusOK,
			Data:       json.RawMessage(`{"match":null}`),
			Errors:     json.RawMessage(`[{"message":"failed"}]`),
		},
	}
	result, err := mustRawService(t, executor, nil).Execute(
		context.Background(),
		RawRequest{Query: `query { match(id: 1) { id } }`},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Partial {
		t.Fatal("data plus errors was not marked partial")
	}
	if result.Extensions != nil {
		t.Fatalf("extensions = %#v, want nil", result.Extensions)
	}
}

func TestRawServiceRejectsMalformedEnvelopeShapes(t *testing.T) {
	tests := []struct {
		name     string
		response *stratz.Response
	}{
		{
			name: "scalar data",
			response: &stratz.Response{
				HTTPStatus: httpStatusOK,
				Data:       json.RawMessage(`true`),
			},
		},
		{
			name: "non-array errors",
			response: &stratz.Response{
				HTTPStatus: httpStatusOK,
				Data:       json.RawMessage(`null`),
				Errors:     json.RawMessage(`{}`),
			},
		},
		{
			name: "non-object error",
			response: &stratz.Response{
				HTTPStatus: httpStatusOK,
				Data:       json.RawMessage(`null`),
				Errors:     json.RawMessage(`[true]`),
			},
		},
		{
			name: "non-object extensions",
			response: &stratz.Response{
				HTTPStatus: httpStatusOK,
				Data:       json.RawMessage(`null`),
				Extensions: json.RawMessage(`[]`),
			},
		},
		{
			name: "invalid status",
			response: &stratz.Response{
				HTTPStatus: 0,
				Data:       json.RawMessage(`null`),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingExecutor{response: test.response}
			_, err := mustRawService(t, executor, nil).Execute(
				context.Background(),
				RawRequest{Query: `query { match(id: 1) { id } }`},
			)
			assertRawCode(t, err, contracts.ErrorCodeUpstreamProtocolError)
		})
	}
}

func TestRawServiceMapsPolicyAndUpstreamErrors(t *testing.T) {
	executor := &recordingExecutor{}
	service := mustRawService(t, executor, nil)
	_, err := service.Execute(
		context.Background(),
		RawRequest{Query: `mutation { match(id: 1) { id } }`},
	)
	assertRawCode(t, err, contracts.ErrorCodeQueryOperationNotAllowed)
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}

	retryAt := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	executor.err = &stratz.Error{
		Code:       contracts.ErrorCodeRateLimited,
		Message:    "limited",
		Retryable:  true,
		RetryAfter: &retryAt,
		Details:    map[string]any{"http_status": 429},
	}
	_, err = service.Execute(
		context.Background(),
		RawRequest{Query: `query { match(id: 1) { id } }`},
	)
	var rawErr *Error
	if !errors.As(err, &rawErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if rawErr.Code != contracts.ErrorCodeRateLimited ||
		!rawErr.Retryable ||
		rawErr.RetryAfter == nil ||
		!rawErr.RetryAfter.Equal(retryAt) {
		t.Fatalf("raw error = %#v", rawErr)
	}
}

func TestRawCachingDisabledByDefaultAndPolicyHook(t *testing.T) {
	executor := &recordingExecutor{
		response: &stratz.Response{
			HTTPStatus: httpStatusOK,
			Data:       json.RawMessage(`{"match":{"id":"1"}}`),
		},
	}
	service := mustRawService(t, executor, nil)
	_, err := service.Execute(context.Background(), RawRequest{
		Query: `query { match(id: 1) { id } }`,
		Cache: true,
	})
	assertRawCode(t, err, contracts.ErrorCodeCacheUnavailable)
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}

	var authorizedTTL time.Duration
	var authorizedAnalysis *policy.Analysis
	hook := CachePolicyFunc(func(analysis *policy.Analysis, ttl time.Duration) error {
		authorizedAnalysis = analysis
		authorizedTTL = ttl
		return nil
	})
	service = mustRawService(t, executor, hook)
	ttl := int64(60)
	if _, err := service.Execute(context.Background(), RawRequest{
		Query:           `query { match(id: 1) { id } }`,
		Cache:           true,
		CacheTTLSeconds: &ttl,
	}); err != nil {
		t.Fatal(err)
	}
	if authorizedTTL != time.Minute || authorizedAnalysis == nil {
		t.Fatalf("cache authorization = %#v, ttl = %s", authorizedAnalysis, authorizedTTL)
	}

	_, err = service.Execute(context.Background(), RawRequest{
		Query:           `query { match(id: 1) { id } }`,
		CacheTTLSeconds: &ttl,
	})
	assertRawCode(t, err, contracts.ErrorCodeInvalidArgument)
}

func mustRawService(
	t *testing.T,
	executor stratz.Executor,
	cachePolicy CachePolicy,
) *RawService {
	t.Helper()
	checker, err := policy.New(policy.Options{
		Limits: config.Defaults(".").Limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRawService(RawOptions{
		Policy:              checker,
		Executor:            executor,
		MaxUpstreamRequests: 5,
		DefaultCacheTTL:     5 * time.Minute,
		CachePolicy:         cachePolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertRawCode(t *testing.T, err error, want contracts.ErrorCode) {
	t.Helper()
	var rawErr *Error
	if !errors.As(err, &rawErr) {
		t.Fatalf("error = %T %v, want raw error %s", err, err, want)
	}
	if rawErr.Code != want {
		t.Fatalf("code = %s, want %s", rawErr.Code, want)
	}
}

const httpStatusOK = 200
