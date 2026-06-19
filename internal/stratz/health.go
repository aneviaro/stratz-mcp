package stratz

import (
	"context"
	"errors"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

const healthOperation = "StratzMCPHealth"

// HealthStatus is the stable connectivity state exposed by doctor and
// stratz_server_info.
type HealthStatus string

const (
	HealthReachable            HealthStatus = "reachable"
	HealthAuthenticationFailed HealthStatus = "authentication_failed"
	HealthWAFBlocked           HealthStatus = "waf_blocked"
	HealthUnreachable          HealthStatus = "unreachable"
	HealthUnchecked            HealthStatus = "unchecked"
)

// Health is a secret-free result from the bounded STRATZ connectivity probe.
type Health struct {
	Status     HealthStatus
	RateLimits []RateLimit
	Error      *Error
}

// Probe performs one authenticated, read-only request. The GraphQL __typename
// meta-field is valid on every object and avoids depending on later generated
// domain operations.
func Probe(ctx context.Context, executor Executor) Health {
	if executor == nil {
		return Health{Status: HealthUnchecked}
	}
	budget, err := NewRequestBudget(1)
	if err != nil {
		return Health{Status: HealthUnreachable}
	}
	response, err := executor.Execute(ctx, budget, Request{
		Query:         "query " + healthOperation + " { __typename }",
		OperationName: healthOperation,
		Mode:          ModeCurated,
		AllowRetries:  false,
	})
	if err == nil {
		if response == nil {
			return Health{Status: HealthUnreachable}
		}
		return Health{
			Status:     HealthReachable,
			RateLimits: append([]RateLimit(nil), response.RateLimits...),
		}
	}

	var upstreamErr *Error
	if !errors.As(err, &upstreamErr) {
		return Health{Status: HealthUnreachable}
	}
	status := HealthUnreachable
	switch upstreamErr.Code {
	case contracts.ErrorCodeAuthenticationFailed:
		status = HealthAuthenticationFailed
	case contracts.ErrorCodeUpstreamWAFBlocked:
		status = HealthWAFBlocked
	}
	return Health{
		Status:     status,
		RateLimits: append([]RateLimit(nil), upstreamErr.RateLimits...),
		Error:      upstreamErr,
	}
}
