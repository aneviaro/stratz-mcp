// Package stratz implements the bounded STRATZ GraphQL transport.
package stratz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

const (
	ProductionEndpoint  = "https://api.stratz.com/graphql"
	acceptMediaTypes    = "application/graphql-response+json, application/json"
	maxRequestBodyBytes = 512 << 10
	maxErrorBodyBytes   = 64 << 10
	maxWireOverhead     = 1 << 20
	maxRetries          = 2
)

// Executor is the bounded request surface consumed by higher-level services.
type Executor interface {
	Execute(context.Context, *RequestBudget, Request) (*Response, error)
}

// Mode controls GraphQL error and partial-result handling.
type Mode uint8

const (
	ModeCurated Mode = iota
	ModeRaw
)

// Request is one already validated, read-only GraphQL operation.
type Request struct {
	Query         string
	Variables     any
	OperationName string
	Mode          Mode
	AllowRetries  bool
}

// GraphQLError is the bounded subset used for stable error classification.
// The complete errors value remains available in Response.Errors.
type GraphQLError struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Response preserves the bounded upstream GraphQL envelope for later
// normalization or raw-response handling.
type Response struct {
	HTTPStatus int
	Data       json.RawMessage
	Errors     json.RawMessage
	Extensions json.RawMessage
	GraphQL    []GraphQLError
	Partial    bool
	RateLimits []RateLimit
	RequestID  string
	Attempts   int
}

// RateLimit is one sanitized observed gateway window.
type RateLimit struct {
	Window    string
	Limit     *int64
	Remaining *int64
	ResetAt   *time.Time
	Source    string
}

// Error is a stable, body-free upstream failure suitable for conversion to
// the public error contract.
type Error struct {
	Code       contracts.ErrorCode
	Message    string
	Retryable  bool
	RetryAfter *time.Time
	Details    map[string]any
	RateLimits []RateLimit
	cause      error
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// RequestBudget is safe for concurrent batch workers.
type RequestBudget struct {
	mu    sync.Mutex
	limit int
	used  int
}

// NewRequestBudget creates a per-MCP-call round-trip budget.
func NewRequestBudget(limit int) (*RequestBudget, error) {
	if limit < 1 || limit > 5 {
		return nil, fmt.Errorf("request budget must be between 1 and 5")
	}
	return &RequestBudget{limit: limit}, nil
}

// Take charges one request attempt to the shared budget.
func (budget *RequestBudget) Take() bool {
	if budget == nil {
		return false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.used >= budget.limit {
		return false
	}
	budget.used++
	return true
}

func (budget *RequestBudget) consume() bool {
	return budget.Take()
}

// Used returns the number of transport attempts charged so far.
func (budget *RequestBudget) Used() int {
	if budget == nil {
		return 0
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.used
}

// Remaining returns the unconsumed round-trip allowance.
func (budget *RequestBudget) Remaining() int {
	if budget == nil {
		return 0
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.limit - budget.used
}

type clientOptions struct {
	endpoint         string
	token            string
	version          string
	timeout          time.Duration
	maxResponseBytes int64
	transport        http.RoundTripper
	now              func() time.Time
	sleep            func(time.Duration) error
	jitter           func(time.Duration) time.Duration
}
