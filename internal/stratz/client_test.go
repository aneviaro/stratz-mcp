package stratz

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

const fixtureToken = "fixture-secret-token"

func TestProductionClientUsesFixedEndpointAndLimits(t *testing.T) {
	limits := config.Defaults(t.TempDir()).Limits
	client, err := New(
		auth.Credential{Token: fixtureToken, Source: auth.SourceEnvironment},
		"1.2.3",
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	if client.endpoint != ProductionEndpoint {
		t.Fatalf("endpoint = %q, want %q", client.endpoint, ProductionEndpoint)
	}
	if client.timeout != limits.UpstreamTimeout {
		t.Fatalf("timeout = %s, want %s", client.timeout, limits.UpstreamTimeout)
	}
	if client.maxResponseBytes != limits.MaxResponseBytes {
		t.Fatalf("response limit = %d, want %d", client.maxResponseBytes, limits.MaxResponseBytes)
	}
}

func TestClientRejectsInvalidConstructionOptions(t *testing.T) {
	valid := clientOptions{
		endpoint:         "https://fixture.invalid/graphql",
		token:            fixtureToken,
		version:          "1.2.3",
		timeout:          5 * time.Second,
		maxResponseBytes: 5 << 20,
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unused")
		}),
	}
	tests := []struct {
		name   string
		mutate func(*clientOptions)
	}{
		{name: "missing endpoint", mutate: func(options *clientOptions) { options.endpoint = "" }},
		{name: "missing token", mutate: func(options *clientOptions) { options.token = "" }},
		{name: "token header injection", mutate: func(options *clientOptions) { options.token = "token\ninjected" }},
		{name: "missing version", mutate: func(options *clientOptions) { options.version = "" }},
		{name: "invalid version characters", mutate: func(options *clientOptions) { options.version = "1.2.3 injected" }},
		{name: "zero timeout", mutate: func(options *clientOptions) { options.timeout = 0 }},
		{name: "excessive timeout", mutate: func(options *clientOptions) { options.timeout = 3 * time.Minute }},
		{name: "zero response limit", mutate: func(options *clientOptions) { options.maxResponseBytes = 0 }},
		{name: "excessive response limit", mutate: func(options *clientOptions) { options.maxResponseBytes = 6 << 20 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.mutate(&options)
			if _, err := newClient(options); err == nil {
				t.Fatal("newClient succeeded")
			}
		})
	}
}

func TestExecuteSendsVerifiedRequestContract(t *testing.T) {
	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen.Add(1)
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		expectedHeaders := map[string]string{
			"Authorization":   "Bearer " + fixtureToken,
			"Content-Type":    "application/json",
			"Accept":          acceptMediaTypes,
			"Accept-Encoding": "gzip",
			"User-Agent":      "stratz-mcp/1.2.3 (+https://github.com/aneviaro/stratz-mcp)",
		}
		for name, expected := range expectedHeaders {
			if actual := request.Header.Get(name); actual != expected {
				t.Errorf("%s = %q, want %q", name, actual, expected)
			}
		}

		var body struct {
			Query         string         `json:"query"`
			Variables     map[string]any `json:"variables"`
			OperationName string         `json:"operationName"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Query != "query Test($id: Int!) { thing(id: $id) }" ||
			body.OperationName != "Test" ||
			body.Variables["id"] != float64(7) {
			t.Errorf("unexpected request body: %#v", body)
		}

		writer.Header().Set("Content-Type", "application/graphql-response+json; charset=utf-8")
		writer.Header().Set("Request-Id", "request-123")
		writer.Header().Set("X-RateLimit-Limit-Second", "8")
		writer.Header().Set("X-RateLimit-Remaining-Second", "7")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, `{"data":{"thing":{"id":7}},"extensions":{"trace":"safe"}}`)
	}))
	defer server.Close()

	client := testClient(t, clientOptions{endpoint: server.URL})
	budget := mustBudget(t, 5)
	response, err := client.Execute(context.Background(), budget, Request{
		Query:         "query Test($id: Int!) { thing(id: $id) }",
		Variables:     map[string]any{"id": 7},
		OperationName: "Test",
		Mode:          ModeCurated,
		AllowRetries:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Load() != 1 || budget.Used() != 1 || response.Attempts != 1 {
		t.Fatalf("attempt accounting = server:%d budget:%d response:%d", seen.Load(), budget.Used(), response.Attempts)
	}
	if response.RequestID != "request-123" {
		t.Fatalf("request ID = %q", response.RequestID)
	}
	if string(response.Data) != `{"thing":{"id":7}}` {
		t.Fatalf("data = %s", response.Data)
	}
	if string(response.Extensions) != `{"trace":"safe"}` {
		t.Fatalf("extensions = %s", response.Extensions)
	}
	if len(response.RateLimits) != 1 ||
		response.RateLimits[0].Limit == nil ||
		*response.RateLimits[0].Limit != 8 {
		t.Fatalf("rate limits = %#v", response.RateLimits)
	}
}

func TestExecuteDecodesGzipWithinBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Encoding", "gzip")
		gzipWriter := gzip.NewWriter(writer)
		_, _ = io.WriteString(gzipWriter, `{"data":{"ok":true}}`)
		_ = gzipWriter.Close()
	}))
	defer server.Close()

	client := testClient(t, clientOptions{endpoint: server.URL})
	response, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Data) != `{"ok":true}` {
		t.Fatalf("data = %s", response.Data)
	}
}

func TestExecuteRejectsGzipBomb(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Encoding", "gzip")
		gzipWriter := gzip.NewWriter(writer)
		_, _ = io.WriteString(gzipWriter, `{"data":"`+strings.Repeat("x", 2048)+`"}`)
		_ = gzipWriter.Close()
	}))
	defer server.Close()

	client := testClient(t, clientOptions{
		endpoint:         server.URL,
		maxResponseBytes: 128,
	})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	assertCode(t, err, contracts.ErrorCodeResponseTooLarge)
}

func TestReadBoundedBodyEnforcesWireLimit(t *testing.T) {
	response := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(strings.Repeat("x", 65))),
	}
	_, err := readBoundedBody(response, 32, 128)
	if err == nil || err.Code != contracts.ErrorCodeResponseTooLarge {
		t.Fatalf("error = %#v, want RESPONSE_TOO_LARGE", err)
	}
}

func TestExecuteRejectsMalformedOrUnsupportedResponses(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		encoding    string
		body        string
	}{
		{name: "malformed JSON", contentType: "application/json", body: `{"data":`},
		{name: "missing envelope", contentType: "application/json", body: `{"unexpected":true}`},
		{name: "trailing JSON", contentType: "application/json", body: `{"data":null} {}`},
		{name: "HTML success", contentType: "text/html", body: `<html>not graphql</html>`},
		{name: "unsupported encoding", contentType: "application/json", encoding: "br", body: `{}`},
		{name: "invalid gzip", contentType: "application/json", encoding: "gzip", body: `not-a-gzip-stream`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", test.contentType)
				if test.encoding != "" {
					writer.Header().Set("Content-Encoding", test.encoding)
				}
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			client := testClient(t, clientOptions{endpoint: server.URL})
			_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
			assertCode(t, err, contracts.ErrorCodeUpstreamProtocolError)
		})
	}
}

func TestExecuteRejectsRedirectWithoutFollowingIt(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := testClient(t, clientOptions{endpoint: source.URL})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	assertCode(t, err, contracts.ErrorCodeUpstreamProtocolError)
	if redirected.Load() {
		t.Fatal("redirect target was contacted")
	}
}

func TestExecuteClassifiesWAFWithoutLeakingHTML(t *testing.T) {
	secretHTML := `<html><title>Just a moment...</title>` + fixtureToken + `</html>`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		writer.Header().Set("Server", "cloudflare")
		writer.Header().Set("cf-mitigated", "challenge")
		writer.Header().Set("cf-ray", "safe-ray")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, secretHTML)
	}))
	defer server.Close()

	client := testClient(t, clientOptions{endpoint: server.URL})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamWAFBlocked)
	if upstreamErr.Retryable {
		t.Fatal("WAF result must be non-retryable for the invocation")
	}
	if strings.Contains(upstreamErr.Error(), fixtureToken) || strings.Contains(upstreamErr.Error(), "Just a moment") {
		t.Fatalf("WAF body leaked through error: %v", upstreamErr)
	}
	if upstreamErr.Details["cf_ray"] != "safe-ray" {
		t.Fatalf("cf_ray = %#v", upstreamErr.Details["cf_ray"])
	}
}

func TestExecuteHandlesPartialGraphQLResultsByMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{
			"data":{"player":{"id":1}},
			"errors":[{"message":"private details","extensions":{"code":"PRIVATE_PROFILE"}}],
			"extensions":{"request":"bounded"}
		}`)
	}))
	defer server.Close()
	client := testClient(t, clientOptions{endpoint: server.URL})

	response, err := client.Execute(context.Background(), mustBudget(t, 1), Request{
		Query: "query Test { player { id } }",
		Mode:  ModeRaw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Partial || len(response.Errors) == 0 || len(response.Data) == 0 {
		t.Fatalf("raw partial response = %#v", response)
	}

	_, err = client.Execute(context.Background(), mustBudget(t, 1), Request{
		Query: "query Test { player { id } }",
		Mode:  ModeCurated,
	})
	assertCode(t, err, contracts.ErrorCodePrivate)
}

func TestExecuteMapsCuratedPartialToPartialError(t *testing.T) {
	server := graphqlServer(t, http.StatusOK, `{
		"data":{"thing":{"id":1}},
		"errors":[{"message":"temporary","extensions":{"code":"INTERNAL_SERVER_ERROR"}}]
	}`)
	defer server.Close()

	client := testClient(t, clientOptions{endpoint: server.URL})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamPartialError)
	if !upstreamErr.Retryable {
		t.Fatal("temporary GraphQL partial error should be retryable")
	}
}

func TestExecuteHTTPAndGraphQLErrorMappings(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header http.Header
		body   string
		mode   Mode
		code   contracts.ErrorCode
		retry  bool
	}{
		{name: "unauthorized", status: 401, body: `{"message":"secret"}`, code: contracts.ErrorCodeAuthenticationFailed},
		{name: "missing token gateway", status: 403, header: http.Header{"WWW-Authenticate": []string{`Key realm="kong"`}}, body: `{"message":"No API key found"}`, code: contracts.ErrorCodeAuthenticationFailed},
		{name: "malformed token gateway quirk", status: 500, body: `{"message":"An unexpected error occurred"}`, code: contracts.ErrorCodeAuthenticationFailed},
		{name: "request timeout", status: 408, body: `{}`, code: contracts.ErrorCodeUpstreamTimeout, retry: true},
		{name: "request too large", status: 413, body: `{}`, code: contracts.ErrorCodeResponseTooLarge},
		{name: "rate limited", status: 429, header: http.Header{"Retry-After": []string{"1"}}, body: `{}`, code: contracts.ErrorCodeRateLimited, retry: true},
		{name: "server error", status: 503, body: `{"message":"unavailable"}`, code: contracts.ErrorCodeUpstreamError, retry: true},
		{name: "internal server error", status: 500, body: `{"message":"different failure"}`, code: contracts.ErrorCodeUpstreamError, retry: true},
		{name: "bad gateway", status: 502, body: `{"message":"unavailable"}`, code: contracts.ErrorCodeUpstreamError, retry: true},
		{name: "gateway timeout", status: 504, body: `{"message":"unavailable"}`, code: contracts.ErrorCodeUpstreamError, retry: true},
		{name: "other server error", status: 507, body: `{"message":"unavailable"}`, code: contracts.ErrorCodeUpstreamError, retry: true},
		{name: "not found fixed endpoint", status: 404, body: `{}`, code: contracts.ErrorCodeUpstreamProtocolError},
		{name: "other client error", status: 418, body: `{}`, code: contracts.ErrorCodeUpstreamProtocolError},
		{name: "raw validation", status: 400, body: `{"errors":[{"message":"bad","extensions":{"code":"SYNTAX_ERROR"}}]}`, mode: ModeRaw, code: contracts.ErrorCodeInvalidArgument},
		{name: "curated validation", status: 400, body: `{"errors":[{"message":"bad","extensions":{"code":"SYNTAX_ERROR"}}]}`, mode: ModeCurated, code: contracts.ErrorCodeUpstreamProtocolError},
		{name: "private forbidden", status: 403, body: `{"errors":[{"message":"private","extensions":{"code":"PRIVATE_PROFILE"}}]}`, code: contracts.ErrorCodePrivate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				for name, values := range test.header {
					for _, value := range values {
						writer.Header().Add(name, value)
					}
				}
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			client := testClient(t, clientOptions{endpoint: server.URL})
			request := validRequest()
			request.Mode = test.mode
			_, err := client.Execute(context.Background(), mustBudget(t, 1), request)
			upstreamErr := assertCode(t, err, test.code)
			if upstreamErr.Retryable != test.retry {
				t.Fatalf("retryable = %t, want %t", upstreamErr.Retryable, test.retry)
			}
			if strings.Contains(upstreamErr.Error(), test.body) ||
				strings.Contains(upstreamErr.Error(), fixtureToken) {
				t.Fatalf("response or token leaked through error: %v", upstreamErr)
			}
		})
	}
}

func TestExecuteClassifiesStreamingFailures(t *testing.T) {
	t.Run("deadline while reading body", func(t *testing.T) {
		client := testClient(t, clientOptions{
			endpoint: "https://fixture.invalid/graphql",
			timeout:  10 * time.Millisecond,
			transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       &contextBody{ctx: request.Context()},
				}, nil
			}),
		})
		_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
		assertCode(t, err, contracts.ErrorCodeUpstreamTimeout)
	})

	t.Run("unexpected EOF while reading body", func(t *testing.T) {
		client := testClient(t, clientOptions{
			endpoint: "https://fixture.invalid/graphql",
			transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body: &errorBody{
						data: []byte(`{"data":`),
						err:  io.ErrUnexpectedEOF,
					},
				}, nil
			}),
		})
		_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
		upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamNetworkError)
		if !upstreamErr.Retryable {
			t.Fatal("unexpected EOF should be retryable")
		}
	})
}

func TestOversizedErrorBodyIsBoundedAndClosed(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader(strings.Repeat("x", maxErrorBodyBytes+1024))}
	client := testClient(t, clientOptions{
		endpoint: "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       body,
			}, nil
		}),
	})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	assertCode(t, err, contracts.ErrorCodeResponseTooLarge)
	if !body.closed.Load() {
		t.Fatal("oversized error response body was not closed")
	}
}

func TestOversizedWAFBodyStillClassifiesAsWAF(t *testing.T) {
	body := `<html><title>Just a moment...</title>` + strings.Repeat("x", maxErrorBodyBytes)
	client := testClient(t, clientOptions{
		endpoint: "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return responseWithBody(http.StatusForbidden, http.Header{
				"Content-Type": []string{"text/html"},
				"Server":       []string{"cloudflare"},
			}, body), nil
		}),
	})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	assertCode(t, err, contracts.ErrorCodeUpstreamWAFBlocked)
}

func TestExecuteClassifiesTransportFailures(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		code  contracts.ErrorCode
		retry bool
	}{
		{
			name:  "temporary DNS",
			err:   &net.DNSError{Err: "temporary fixture", Name: "fixture.invalid", IsTemporary: true},
			code:  contracts.ErrorCodeUpstreamNetworkError,
			retry: true,
		},
		{
			name:  "connection reset",
			err:   io.ErrUnexpectedEOF,
			code:  contracts.ErrorCodeUpstreamNetworkError,
			retry: true,
		},
		{
			name: "TLS certificate",
			err: &tls.CertificateVerificationError{
				Err: errors.New("fixture certificate failure"),
			},
			code: contracts.ErrorCodeUpstreamTLSError,
		},
		{
			name: "permanent network",
			err:  errors.New("permanent fixture failure"),
			code: contracts.ErrorCodeUpstreamNetworkError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, clientOptions{
				endpoint:  "https://fixture.invalid/graphql",
				transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, test.err }),
			})
			_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
			upstreamErr := assertCode(t, err, test.code)
			if upstreamErr.Retryable != test.retry {
				t.Fatalf("retryable = %t, want %t", upstreamErr.Retryable, test.retry)
			}
		})
	}
}

func TestExecuteClassifiesRealTLSValidationFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	client := testClient(t, clientOptions{endpoint: server.URL})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	assertCode(t, err, contracts.ErrorCodeUpstreamTLSError)
}

func TestExecuteTimeoutAndCancellation(t *testing.T) {
	blockingTransport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	t.Run("timeout", func(t *testing.T) {
		client := testClient(t, clientOptions{
			endpoint:  "https://fixture.invalid/graphql",
			transport: blockingTransport,
			timeout:   10 * time.Millisecond,
		})
		_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
		upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamTimeout)
		if !upstreamErr.Retryable || !errors.Is(upstreamErr, context.DeadlineExceeded) {
			t.Fatalf("timeout error = %#v", upstreamErr)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		client := testClient(t, clientOptions{
			endpoint:  "https://fixture.invalid/graphql",
			transport: blockingTransport,
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.Execute(ctx, mustBudget(t, 1), validRequest())
		upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamNetworkError)
		if upstreamErr.Retryable || !errors.Is(upstreamErr, context.Canceled) {
			t.Fatalf("cancellation error = %#v", upstreamErr)
		}
	})
}

func TestExecuteRetriesWithJitterAndRetryAfter(t *testing.T) {
	now := time.Now().UTC()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		current := attempts.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		if current == 1 {
			writer.Header().Set("Retry-After", "2")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{}`)
			return
		}
		_, _ = io.WriteString(writer, `{"data":{"ok":true}}`)
	}))
	defer server.Close()

	var sleeps []time.Duration
	client := testClient(t, clientOptions{
		endpoint: server.URL,
		now:      func() time.Time { return now },
		jitter:   func(time.Duration) time.Duration { return 25 * time.Millisecond },
		sleep: func(duration time.Duration) error {
			sleeps = append(sleeps, duration)
			return nil
		},
		timeout: 5 * time.Second,
	})
	request := validRequest()
	request.AllowRetries = true
	response, err := client.Execute(context.Background(), mustBudget(t, 5), request)
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || response.Attempts != 2 {
		t.Fatalf("attempts = server:%d response:%d", attempts.Load(), response.Attempts)
	}
	if len(sleeps) != 1 || sleeps[0] != 2*time.Second {
		t.Fatalf("sleeps = %v, want [2s]", sleeps)
	}
}

func TestExecuteRetriesTemporaryFailuresAtMostTwice(t *testing.T) {
	var attempts atomic.Int32
	client := testClient(t, clientOptions{
		endpoint: "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return nil, io.ErrUnexpectedEOF
		}),
		jitter: func(time.Duration) time.Duration { return 0 },
		sleep:  func(time.Duration) error { return nil },
	})
	request := validRequest()
	request.AllowRetries = true
	_, err := client.Execute(context.Background(), mustBudget(t, 5), request)
	assertCode(t, err, contracts.ErrorCodeUpstreamNetworkError)
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
}

func TestWAFUsesAtMostOneDiagnosticRetry(t *testing.T) {
	var attempts atomic.Int32
	client := testClient(t, clientOptions{
		endpoint: "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return responseWithBody(http.StatusForbidden, http.Header{
				"Content-Type": []string{"text/html"},
				"Server":       []string{"cloudflare"},
				"cf-mitigated": []string{"challenge"},
			}, `<html>Just a moment...</html>`), nil
		}),
		jitter: func(time.Duration) time.Duration { return 0 },
		sleep:  func(time.Duration) error { return nil },
	})
	request := validRequest()
	request.AllowRetries = true
	_, err := client.Execute(context.Background(), mustBudget(t, 5), request)
	upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamWAFBlocked)
	if upstreamErr.Retryable {
		t.Fatal("final WAF error must be non-retryable")
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestRequestBudgetStopsRetriesAndFurtherCalls(t *testing.T) {
	var attempts atomic.Int32
	client := testClient(t, clientOptions{
		endpoint: "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return responseWithBody(
				http.StatusServiceUnavailable,
				http.Header{"Content-Type": []string{"application/json"}},
				`{"message":"unavailable"}`,
			), nil
		}),
		jitter: func(time.Duration) time.Duration { return 0 },
		sleep:  func(time.Duration) error { return nil },
	})
	budget := mustBudget(t, 2)
	request := validRequest()
	request.AllowRetries = true
	_, err := client.Execute(context.Background(), budget, request)
	assertCode(t, err, contracts.ErrorCodeUpstreamError)
	if attempts.Load() != 2 || budget.Used() != 2 || budget.Remaining() != 0 {
		t.Fatalf("budget accounting = attempts:%d used:%d remaining:%d", attempts.Load(), budget.Used(), budget.Remaining())
	}

	_, err = client.Execute(context.Background(), budget, validRequest())
	assertCode(t, err, contracts.ErrorCodeRequestBudgetExceeded)
	if attempts.Load() != 2 {
		t.Fatalf("exhausted budget made another request: %d attempts", attempts.Load())
	}
}

func TestResponseBodyIsClosedAndErrorBodiesDoNotLeak(t *testing.T) {
	body := &trackingBody{
		Reader: strings.NewReader(`{"message":"upstream secret ` + fixtureToken + `"}`),
	}
	client := testClient(t, clientOptions{
		endpoint: "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-SteamId":    []string{"private-account"},
				},
				Body: body,
			}, nil
		}),
	})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	upstreamErr := assertCode(t, err, contracts.ErrorCodeUpstreamError)
	if !body.closed.Load() {
		t.Fatal("response body was not closed")
	}
	rendered := upstreamErr.Error()
	allErrorData := rendered + fmt.Sprint(upstreamErr.Details, upstreamErr.RateLimits)
	if strings.Contains(allErrorData, fixtureToken) ||
		strings.Contains(allErrorData, "private-account") ||
		strings.Contains(allErrorData, "upstream secret") {
		t.Fatalf("sensitive response data leaked: %s", allErrorData)
	}
}

func TestRateLimitsArePreservedOnErrors(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-RateLimit-Limit-Minute", "150")
		writer.Header().Set("X-RateLimit-Remaining-Minute", "0")
		writer.Header().Set("RateLimit-Limit", "8")
		writer.Header().Set("RateLimit-Remaining", "0")
		writer.Header().Set("RateLimit-Reset", "2")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()

	client := testClient(t, clientOptions{
		endpoint: server.URL,
		now:      func() time.Time { return now },
	})
	_, err := client.Execute(context.Background(), mustBudget(t, 1), validRequest())
	upstreamErr := assertCode(t, err, contracts.ErrorCodeRateLimited)
	if len(upstreamErr.RateLimits) != 2 {
		t.Fatalf("rate limits = %#v", upstreamErr.RateLimits)
	}
	if upstreamErr.RetryAfter == nil || !upstreamErr.RetryAfter.Equal(now.Add(2*time.Second)) {
		t.Fatalf("retry after = %#v", upstreamErr.RetryAfter)
	}
}

func TestRequestValidationDoesNotSpendBudget(t *testing.T) {
	client := testClient(t, clientOptions{
		endpoint:  "https://fixture.invalid/graphql",
		transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }),
	})
	tests := []Request{
		{},
		{Query: "query Test { __typename }", Mode: Mode(255)},
		{Query: "query Test($x: String!) { thing(x: $x) }", Variables: map[string]any{"x": make(chan int)}},
		{Query: "query Test { __typename } #" + strings.Repeat("x", maxRequestBodyBytes)},
	}
	for _, request := range tests {
		budget := mustBudget(t, 1)
		_, err := client.Execute(context.Background(), budget, request)
		assertCode(t, err, contracts.ErrorCodeInvalidArgument)
		if budget.Used() != 0 {
			t.Fatalf("invalid request spent budget: %d", budget.Used())
		}
	}
}

func TestRequestBudgetValidationAndConcurrency(t *testing.T) {
	for _, invalid := range []int{-1, 0, 6} {
		if _, err := NewRequestBudget(invalid); err == nil {
			t.Fatalf("NewRequestBudget(%d) succeeded", invalid)
		}
	}
	budget := mustBudget(t, 5)
	results := make(chan bool, 20)
	for range 20 {
		go func() {
			results <- budget.consume()
		}()
	}
	successes := 0
	for range 20 {
		if <-results {
			successes++
		}
	}
	if successes != 5 || budget.Used() != 5 || budget.Remaining() != 0 {
		t.Fatalf("concurrent budget = successes:%d used:%d remaining:%d", successes, budget.Used(), budget.Remaining())
	}
}

func testClient(t *testing.T, options clientOptions) *Client {
	t.Helper()
	if options.endpoint == "" {
		options.endpoint = "https://fixture.invalid/graphql"
	}
	if options.token == "" {
		options.token = fixtureToken
	}
	if options.version == "" {
		options.version = "1.2.3"
	}
	if options.timeout == 0 {
		options.timeout = 5 * time.Second
	}
	if options.maxResponseBytes == 0 {
		options.maxResponseBytes = 5 << 20
	}
	client, err := newClient(options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func validRequest() Request {
	return Request{
		Query:         "query Test { __typename }",
		OperationName: "Test",
		Mode:          ModeCurated,
	}
}

func mustBudget(t *testing.T, limit int) *RequestBudget {
	t.Helper()
	budget, err := NewRequestBudget(limit)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func assertCode(t *testing.T, err error, code contracts.ErrorCode) *Error {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	var upstreamErr *Error
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("error type = %T, want *stratz.Error", err)
	}
	if upstreamErr.Code != code {
		t.Fatalf("error code = %s, want %s (%v)", upstreamErr.Code, code, err)
	}
	return upstreamErr
}

func graphqlServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
}

func responseWithBody(status int, headers http.Header, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (body *trackingBody) Close() error {
	body.closed.Store(true)
	return nil
}

type contextBody struct {
	ctx context.Context
}

func (body *contextBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (*contextBody) Close() error {
	return nil
}

type errorBody struct {
	data []byte
	err  error
}

func (body *errorBody) Read(buffer []byte) (int, error) {
	if len(body.data) == 0 {
		return 0, body.err
	}
	count := copy(buffer, body.data)
	body.data = body.data[count:]
	return count, nil
}

func (*errorBody) Close() error {
	return nil
}
