package doctor

import (
	"context"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

type diagnosticExecutor struct {
	response *stratz.Response
	err      error
}

func (executor diagnosticExecutor) Execute(
	context.Context,
	*stratz.RequestBudget,
	stratz.Request,
) (*stratz.Response, error) {
	return executor.response, executor.err
}

func TestDiagnoseReportsReachabilitySchemaAndCache(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.Cache.Enabled = false
	report := Diagnose(context.Background(), Options{
		Config:        cfg,
		Cache:         nil,
		Executor:      diagnosticExecutor{response: &stratz.Response{}},
		SchemaVersion: "sha256:fixture",
	})
	if report.HasErrors() {
		t.Fatalf("report unexpectedly has errors: %#v", report.Findings)
	}
	assertFinding(t, report, "upstream_reachable", SeverityInfo)
	assertFinding(t, report, "schema_available", SeverityInfo)
	assertFinding(t, report, "cache_disabled", SeverityInfo)
}

func TestDiagnoseDistinguishesAuthenticationAndWAF(t *testing.T) {
	tests := []struct {
		name string
		code contracts.ErrorCode
		want string
	}{
		{"authentication", contracts.ErrorCodeAuthenticationFailed, "upstream_authentication_failed"},
		{"waf", contracts.ErrorCodeUpstreamWAFBlocked, "upstream_waf_blocked"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Defaults(t.TempDir())
			cfg.Cache.Enabled = false
			report := Diagnose(context.Background(), Options{
				Config: cfg,
				Cache:  nil,
				Executor: diagnosticExecutor{err: &stratz.Error{
					Code:    test.code,
					Message: "safe",
				}},
				SchemaVersion: "sha256:fixture",
			})
			if !report.HasErrors() {
				t.Fatal("report did not fail")
			}
			assertFinding(t, report, test.want, SeverityError)
		})
	}
}

func TestDiagnoseReportsHealthyAndDegradedCacheStates(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.Cache.Enabled = true

	healthyCache, err := cache.Open(cache.Options{
		Config:   cfg.Cache,
		Features: cfg.Features,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer healthyCache.Close()
	healthy := Diagnose(context.Background(), Options{
		Config:        cfg,
		Cache:         healthyCache,
		Executor:      diagnosticExecutor{response: &stratz.Response{}},
		SchemaVersion: "sha256:fixture",
	})
	assertFinding(t, healthy, "cache_healthy", SeverityInfo)

	degraded := Diagnose(context.Background(), Options{
		Config:        cfg,
		Cache:         cache.Degraded(nil, "fixture"),
		Executor:      diagnosticExecutor{response: &stratz.Response{}},
		SchemaVersion: "sha256:fixture",
	})
	assertFinding(t, degraded, "cache_degraded", SeverityWarning)
}

func assertFinding(t *testing.T, report Report, code string, severity Severity) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.Code == code {
			if finding.Severity != severity {
				t.Fatalf("%s severity = %q, want %q", code, finding.Severity, severity)
			}
			return
		}
	}
	t.Fatalf("missing finding %q in %#v", code, report.Findings)
}
