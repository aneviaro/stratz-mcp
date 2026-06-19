package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/graphql/policy"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

// RawRequest is the validated public input for stratz_execute_graphql.
type RawRequest struct {
	Query           string
	Variables       map[string]any
	OperationName   string
	Cache           bool
	CacheTTLSeconds *int64
	Fresh           bool
}

// RawResult is the normalized raw GraphQL success payload.
type RawResult struct {
	Data       any
	Errors     []map[string]any
	Extensions any
	HTTPStatus int
	Partial    bool
	RateLimits []stratz.RateLimit
}

// Error is a stable raw-tool execution failure.
type Error struct {
	Code       contracts.ErrorCode
	Message    string
	Retryable  bool
	RetryAfter *time.Time
	Details    map[string]any
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// CachePolicy is the future cache/classification integration seam. The v1
// default rejects raw cache requests until field classifications are approved.
type CachePolicy interface {
	Authorize(*policy.Analysis, time.Duration) error
}

// CachePolicyFunc adapts a function into a cache policy.
type CachePolicyFunc func(*policy.Analysis, time.Duration) error

func (function CachePolicyFunc) Authorize(
	analysis *policy.Analysis,
	ttl time.Duration,
) error {
	return function(analysis, ttl)
}

// RawOptions configures raw execution.
type RawOptions struct {
	Policy              *policy.Policy
	Executor            stratz.Executor
	MaxUpstreamRequests int
	DefaultCacheTTL     time.Duration
	CachePolicy         CachePolicy
}

// RawService validates and executes one guarded raw operation.
type RawService struct {
	policy              *policy.Policy
	executor            stratz.Executor
	maxUpstreamRequests int
	defaultCacheTTL     time.Duration
	cachePolicy         CachePolicy
}

// NewRawService creates a guarded raw executor.
func NewRawService(options RawOptions) (*RawService, error) {
	if options.Policy == nil {
		return nil, errors.New("raw GraphQL policy is required")
	}
	if options.Executor == nil {
		return nil, errors.New("STRATZ executor is required")
	}
	if options.MaxUpstreamRequests < 1 || options.MaxUpstreamRequests > 5 {
		return nil, errors.New("raw GraphQL request budget must be between 1 and 5")
	}
	if options.DefaultCacheTTL <= 0 || options.DefaultCacheTTL > time.Hour {
		return nil, errors.New("raw GraphQL cache TTL must be between 1ns and 1h")
	}
	cachePolicy := options.CachePolicy
	if cachePolicy == nil {
		cachePolicy = disabledRawCachePolicy{}
	}
	return &RawService{
		policy:              options.Policy,
		executor:            options.Executor,
		maxUpstreamRequests: options.MaxUpstreamRequests,
		defaultCacheTTL:     options.DefaultCacheTTL,
		cachePolicy:         cachePolicy,
	}, nil
}

// Execute validates and sends one canonical raw query.
func (service *RawService) Execute(
	ctx context.Context,
	request RawRequest,
) (*RawResult, error) {
	if request.CacheTTLSeconds != nil && !request.Cache {
		return nil, rawError(
			contracts.ErrorCodeInvalidArgument,
			"cache_ttl_seconds requires cache=true",
			nil,
		)
	}
	analysis, err := service.policy.Analyze(policy.Request{
		Query:         request.Query,
		Variables:     request.Variables,
		OperationName: request.OperationName,
		Cache:         request.Cache,
	})
	if err != nil {
		var policyErr *policy.Error
		if errors.As(err, &policyErr) {
			return nil, &Error{
				Code:      policyErr.Code,
				Message:   policyErr.Message,
				Details:   policyErr.Details,
				Retryable: false,
			}
		}
		return nil, rawError(
			contracts.ErrorCodeInternalError,
			"Raw GraphQL policy failed internally",
			nil,
		)
	}

	if request.Cache {
		ttl := service.defaultCacheTTL
		if request.CacheTTLSeconds != nil {
			ttl = time.Duration(*request.CacheTTLSeconds) * time.Second
		}
		if ttl <= 0 || ttl > time.Hour {
			return nil, rawError(
				contracts.ErrorCodeInvalidArgument,
				"Raw GraphQL cache TTL must be between 1 and 3600 seconds",
				map[string]any{"ttl_seconds": int64(ttl / time.Second)},
			)
		}
		if cacheErr := service.cachePolicy.Authorize(analysis, ttl); cacheErr != nil {
			var rawErr *Error
			if errors.As(cacheErr, &rawErr) {
				return nil, rawErr
			}
			return nil, rawError(
				contracts.ErrorCodeCacheUnavailable,
				"Raw GraphQL caching is unavailable",
				nil,
			)
		}
	}

	budget, budgetErr := stratz.NewRequestBudget(service.maxUpstreamRequests)
	if budgetErr != nil {
		return nil, rawError(
			contracts.ErrorCodeInternalError,
			"Raw GraphQL request budget is invalid",
			nil,
		)
	}
	response, executeErr := service.executor.Execute(ctx, budget, stratz.Request{
		Query:         analysis.Query,
		Variables:     analysis.Variables,
		OperationName: analysis.OperationName,
		Mode:          stratz.ModeRaw,
		AllowRetries:  true,
	})
	if executeErr != nil {
		var upstreamErr *stratz.Error
		if errors.As(executeErr, &upstreamErr) {
			return nil, &Error{
				Code:       upstreamErr.Code,
				Message:    upstreamErr.Message,
				Retryable:  upstreamErr.Retryable,
				RetryAfter: upstreamErr.RetryAfter,
				Details:    upstreamErr.Details,
			}
		}
		return nil, rawError(
			contracts.ErrorCodeInternalError,
			"Raw GraphQL execution failed internally",
			nil,
		)
	}
	if response == nil {
		return nil, rawError(
			contracts.ErrorCodeUpstreamProtocolError,
			"STRATZ returned no GraphQL response",
			nil,
		)
	}
	result, normalizeErr := normalizeRawResponse(response)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	return result, nil
}

func normalizeRawResponse(response *stratz.Response) (*RawResult, error) {
	data, err := decodeRawValue(response.Data, nil)
	if err != nil {
		return nil, protocolResponseError("data")
	}
	switch data.(type) {
	case nil, map[string]any, []any:
	default:
		return nil, protocolResponseError("data")
	}

	errorsValue, err := decodeRawValue(response.Errors, []any{})
	if err != nil {
		return nil, protocolResponseError("errors")
	}
	errorItems, ok := errorsValue.([]any)
	if !ok || len(errorItems) > 100 {
		return nil, protocolResponseError("errors")
	}
	publicErrors := make([]map[string]any, 0, len(errorItems))
	for _, item := range errorItems {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, protocolResponseError("errors")
		}
		publicErrors = append(publicErrors, object)
	}

	extensions, err := decodeRawValue(response.Extensions, nil)
	if err != nil {
		return nil, protocolResponseError("extensions")
	}
	switch extensions.(type) {
	case nil, map[string]any:
	default:
		return nil, protocolResponseError("extensions")
	}
	if response.HTTPStatus < 100 || response.HTTPStatus > 599 {
		return nil, protocolResponseError("http_status")
	}
	return &RawResult{
		Data:       data,
		Errors:     publicErrors,
		Extensions: extensions,
		HTTPStatus: response.HTTPStatus,
		Partial:    response.Partial || data != nil && len(publicErrors) > 0,
		RateLimits: response.RateLimits,
	}, nil
}

func decodeRawValue(raw json.RawMessage, fallback any) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fallback, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func protocolResponseError(field string) *Error {
	return rawError(
		contracts.ErrorCodeUpstreamProtocolError,
		"STRATZ returned a GraphQL response outside the raw-tool contract",
		map[string]any{"field": field},
	)
}

func rawError(
	code contracts.ErrorCode,
	message string,
	details map[string]any,
) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{
		Code:      code,
		Message:   message,
		Details:   details,
		Retryable: false,
	}
}

type disabledRawCachePolicy struct{}

func (disabledRawCachePolicy) Authorize(
	analysis *policy.Analysis,
	_ time.Duration,
) error {
	details := map[string]any{
		"classification_approved": false,
	}
	if analysis != nil {
		details["cacheable"] = analysis.Cacheable
		if len(analysis.SensitiveFields) > 0 {
			details["sensitive_fields"] = analysis.SensitiveFields
		}
	}
	return rawError(
		contracts.ErrorCodeCacheUnavailable,
		"Raw GraphQL caching is disabled until field classifications are approved",
		details,
	)
}

func (result *RawResult) String() string {
	return fmt.Sprintf(
		"raw GraphQL status=%d partial=%t errors=%d",
		result.HTTPStatus,
		result.Partial,
		len(result.Errors),
	)
}
