package stratz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/auth"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

// Client executes bounded requests against the fixed production endpoint.
type Client struct {
	endpoint         string
	token            string
	userAgent        string
	timeout          time.Duration
	maxResponseBytes int64
	maxWireBytes     int64
	httpClient       *http.Client
	now              func() time.Time
	sleep            func(context.Context, time.Duration) error
	jitter           func(time.Duration) time.Duration
}

var _ Executor = (*Client)(nil)

// New constructs the production STRATZ client. Endpoint and transport
// replacement are intentionally unavailable through this constructor.
func New(credential auth.Credential, version string, limits config.LimitsConfig) (*Client, error) {
	return newClient(clientOptions{
		endpoint:         ProductionEndpoint,
		token:            credential.Token,
		version:          version,
		timeout:          limits.UpstreamTimeout,
		maxResponseBytes: limits.MaxResponseBytes,
	})
}

func newClient(options clientOptions) (*Client, error) {
	if options.endpoint == "" {
		return nil, errors.New("STRATZ endpoint is required")
	}
	if options.token == "" || hasHeaderControl(options.token) {
		return nil, errors.New("STRATZ token is invalid")
	}
	version := strings.TrimSpace(options.version)
	if !validUserAgentVersion(version) {
		return nil, errors.New("STRATZ client version is invalid")
	}
	if options.timeout <= 0 || options.timeout > 2*time.Minute {
		return nil, errors.New("STRATZ timeout must be between 1ns and 2m")
	}
	if options.maxResponseBytes <= 0 || options.maxResponseBytes > 5<<20 {
		return nil, errors.New("STRATZ response limit must be between 1 byte and 5 MiB")
	}

	transport := options.transport
	if transport == nil {
		defaultTransport := http.DefaultTransport.(*http.Transport).Clone()
		defaultTransport.DisableCompression = true
		transport = defaultTransport
	}
	now := options.now
	if now == nil {
		now = time.Now
	}
	jitter := options.jitter
	if jitter == nil {
		jitter = func(maximum time.Duration) time.Duration {
			if maximum <= 0 {
				return 0
			}
			return time.Duration(rand.Int64N(int64(maximum) + 1))
		}
	}

	client := &Client{
		endpoint:         options.endpoint,
		token:            options.token,
		userAgent:        "stratz-mcp/" + version + " (+https://github.com/aneviaro/stratz-mcp)",
		timeout:          options.timeout,
		maxResponseBytes: options.maxResponseBytes,
		maxWireBytes:     options.maxResponseBytes + maxWireOverhead,
		now:              now,
		jitter:           jitter,
	}
	client.httpClient = &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if options.sleep != nil {
		client.sleep = func(_ context.Context, duration time.Duration) error {
			return options.sleep(duration)
		}
	} else {
		client.sleep = sleepContext
	}
	return client, nil
}

// Execute sends one bounded GraphQL request using the shared per-call budget.
func (client *Client) Execute(parent context.Context, budget *RequestBudget, request Request) (*Response, error) {
	if budget == nil {
		return nil, requestBudgetError()
	}
	body, err := encodeRequest(request)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(parent, client.timeout)
	defer cancel()

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, classifyContextError(err)
		}
		if !budget.consume() {
			return nil, requestBudgetError()
		}

		response, requestErr := client.executeAttempt(ctx, body, request.Mode)
		if requestErr == nil {
			response.Attempts = attempt + 1
			return response, nil
		}
		if !client.shouldRetry(request, requestErr, attempt) {
			return nil, requestErr
		}
		if budget.Remaining() == 0 {
			return nil, requestBudgetError()
		}

		delay, ok := client.retryDelay(ctx, requestErr, attempt)
		if !ok {
			return nil, requestErr
		}
		if err := client.sleep(ctx, delay); err != nil {
			return nil, classifyContextError(err)
		}
	}
}

func (client *Client) executeAttempt(ctx context.Context, body []byte, mode Mode) (*Response, *Error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, protocolError("could not create the upstream request", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+client.token)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", acceptMediaTypes)
	httpRequest.Header.Set("Accept-Encoding", "gzip")
	httpRequest.Header.Set("User-Agent", client.userAgent)

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return nil, classifyTransportError(ctx, err)
	}
	defer httpResponse.Body.Close()

	bodyLimit := client.maxResponseBytes
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		bodyLimit = min(bodyLimit, int64(maxErrorBodyBytes))
	}
	rateLimits := parseRateLimits(httpResponse.Header, client.now())
	requestID := safeHeaderValue(firstHeader(
		httpResponse.Header,
		"Request-Id",
		"X-Request-Id",
	))
	decoded, readErr := readBoundedBody(httpResponse, client.maxWireBytes, bodyLimit)
	if readErr != nil {
		if waf := classifyWAF(httpResponse, decoded); waf != nil {
			waf.Details = responseDetails(httpResponse, requestID)
			waf.RateLimits = rateLimits
			return nil, waf
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, classifyContextError(ctxErr)
		}
		if readErr.cause != nil &&
			(isTLSError(readErr.cause) || isTemporaryNetworkError(readErr.cause)) {
			return nil, classifyTransportError(ctx, readErr.cause)
		}
		readErr.Details = responseDetails(httpResponse, requestID)
		readErr.RateLimits = rateLimits
		return nil, readErr
	}

	if waf := classifyWAF(httpResponse, decoded); waf != nil {
		waf.Details = responseDetails(httpResponse, requestID)
		waf.RateLimits = rateLimits
		return nil, waf
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return nil, classifyHTTPError(httpResponse, decoded, mode, rateLimits, requestID, client.now())
	}
	if !isJSONMediaType(headerValue(httpResponse.Header, "Content-Type")) {
		return nil, withRateLimits(protocolErrorWithDetails(
			"STRATZ returned an unexpected response media type",
			responseDetails(httpResponse, requestID),
			nil,
		), rateLimits)
	}

	envelope, decodeErr := decodeGraphQL(decoded)
	if decodeErr != nil {
		return nil, withRateLimits(protocolErrorWithDetails(
			"STRATZ returned malformed GraphQL JSON",
			responseDetails(httpResponse, requestID),
			decodeErr,
		), rateLimits)
	}
	if len(envelope.graphqlErrors) > 0 {
		if mode == ModeRaw && envelope.hasData {
			return &Response{
				HTTPStatus: httpResponse.StatusCode,
				Data:       envelope.data,
				Errors:     envelope.errors,
				Extensions: envelope.extensions,
				GraphQL:    envelope.graphqlErrors,
				Partial:    true,
				RateLimits: rateLimits,
				RequestID:  requestID,
			}, nil
		}
		return nil, classifyGraphQLError(
			httpResponse.StatusCode,
			envelope,
			mode,
			rateLimits,
			requestID,
		)
	}

	return &Response{
		HTTPStatus: httpResponse.StatusCode,
		Data:       envelope.data,
		Errors:     envelope.errors,
		Extensions: envelope.extensions,
		GraphQL:    envelope.graphqlErrors,
		RateLimits: rateLimits,
		RequestID:  requestID,
	}, nil
}

func encodeRequest(request Request) ([]byte, error) {
	if strings.TrimSpace(request.Query) == "" {
		return nil, invalidArgumentError("GraphQL query is required")
	}
	if request.Mode != ModeCurated && request.Mode != ModeRaw {
		return nil, invalidArgumentError("GraphQL request mode is invalid")
	}
	variables := request.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	body, err := json.Marshal(struct {
		Query         string `json:"query"`
		Variables     any    `json:"variables"`
		OperationName string `json:"operationName,omitempty"`
	}{
		Query:         request.Query,
		Variables:     variables,
		OperationName: request.OperationName,
	})
	if err != nil {
		return nil, invalidArgumentError("GraphQL variables are not JSON-compatible")
	}
	if len(body) > maxRequestBodyBytes {
		return nil, invalidArgumentError("GraphQL request body exceeds 512 KiB")
	}
	return body, nil
}

func (client *Client) shouldRetry(request Request, upstreamErr *Error, attempt int) bool {
	if !request.AllowRetries || attempt >= maxRetries {
		return false
	}
	if upstreamErr.Code == contracts.ErrorCodeUpstreamWAFBlocked {
		return attempt == 0
	}
	return upstreamErr.Retryable
}

func (client *Client) retryDelay(ctx context.Context, upstreamErr *Error, attempt int) (time.Duration, bool) {
	maximum := 100 * time.Millisecond * time.Duration(1<<attempt)
	if maximum > 2*time.Second {
		maximum = 2 * time.Second
	}
	delay := client.jitter(maximum)
	now := client.now()
	if upstreamErr.RetryAfter != nil {
		untilReset := upstreamErr.RetryAfter.Sub(now)
		if untilReset > delay {
			delay = untilReset
		}
	}
	if deadline, ok := ctx.Deadline(); ok && !now.Add(delay).Before(deadline) {
		return 0, false
	}
	return delay, true
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func hasHeaderControl(value string) bool {
	for _, character := range value {
		if character == '\r' || character == '\n' || character == 0 {
			return true
		}
	}
	return false
}

func validUserAgentVersion(value string) bool {
	if value == "" || len(value) > 64 || hasHeaderControl(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '.' && character != '_' &&
			character != '+' && character != '-' {
			return false
		}
	}
	return true
}

func invalidArgumentError(message string) *Error {
	return &Error{
		Code:      contracts.ErrorCodeInvalidArgument,
		Message:   message,
		Details:   map[string]any{},
		Retryable: false,
	}
}

func requestBudgetError() *Error {
	return &Error{
		Code:      contracts.ErrorCodeRequestBudgetExceeded,
		Message:   "The upstream request budget is exhausted",
		Details:   map[string]any{},
		Retryable: false,
	}
}

func protocolError(message string, cause error) *Error {
	return protocolErrorWithDetails(message, map[string]any{}, cause)
}

func protocolErrorWithDetails(message string, details map[string]any, cause error) *Error {
	return &Error{
		Code:      contracts.ErrorCodeUpstreamProtocolError,
		Message:   message,
		Details:   details,
		Retryable: false,
		cause:     cause,
	}
}

func withRateLimits(upstreamErr *Error, rates []RateLimit) *Error {
	upstreamErr.RateLimits = rates
	return upstreamErr
}

func min(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
