package stratz

import (
	"context"
	"errors"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

type probeExecutor func(context.Context, *RequestBudget, Request) (*Response, error)

func (executor probeExecutor) Execute(
	ctx context.Context,
	budget *RequestBudget,
	request Request,
) (*Response, error) {
	return executor(ctx, budget, request)
}

func TestProbeUsesOneBoundedReadOnlyRequest(t *testing.T) {
	executor := probeExecutor(func(
		_ context.Context,
		budget *RequestBudget,
		request Request,
	) (*Response, error) {
		if request.OperationName != healthOperation {
			t.Fatalf("operation = %q", request.OperationName)
		}
		if request.AllowRetries {
			t.Fatal("health probe enabled retries")
		}
		if request.Mode != ModeCurated {
			t.Fatalf("mode = %v", request.Mode)
		}
		if budget.Remaining() != 1 {
			t.Fatalf("budget remaining = %d, want 1", budget.Remaining())
		}
		return &Response{RateLimits: []RateLimit{{Window: "minute"}}}, nil
	})

	health := Probe(context.Background(), executor)
	if health.Status != HealthReachable {
		t.Fatalf("status = %q", health.Status)
	}
	if len(health.RateLimits) != 1 {
		t.Fatalf("rate limits = %#v", health.RateLimits)
	}
}

func TestProbeClassifiesStableConnectivityStates(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want HealthStatus
	}{
		{
			name: "authentication",
			err: &Error{
				Code:    contracts.ErrorCodeAuthenticationFailed,
				Message: "rejected",
			},
			want: HealthAuthenticationFailed,
		},
		{
			name: "waf",
			err: &Error{
				Code:    contracts.ErrorCodeUpstreamWAFBlocked,
				Message: "blocked",
			},
			want: HealthWAFBlocked,
		},
		{
			name: "network",
			err: &Error{
				Code:    contracts.ErrorCodeUpstreamNetworkError,
				Message: "unreachable",
			},
			want: HealthUnreachable,
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: HealthUnreachable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			health := Probe(context.Background(), probeExecutor(func(
				context.Context,
				*RequestBudget,
				Request,
			) (*Response, error) {
				return nil, test.err
			}))
			if health.Status != test.want {
				t.Fatalf("status = %q, want %q", health.Status, test.want)
			}
		})
	}
}

func TestProbeWithoutExecutorIsUnchecked(t *testing.T) {
	if status := Probe(context.Background(), nil).Status; status != HealthUnchecked {
		t.Fatalf("status = %q, want %q", status, HealthUnchecked)
	}
}

func TestProbeRejectsNilSuccessResponse(t *testing.T) {
	health := Probe(context.Background(), probeExecutor(func(
		context.Context,
		*RequestBudget,
		Request,
	) (*Response, error) {
		return nil, nil
	}))
	if health.Status != HealthUnreachable {
		t.Fatalf("status = %q, want %q", health.Status, HealthUnreachable)
	}
}
