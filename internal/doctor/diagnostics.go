package doctor

import (
	"context"
	"fmt"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/cache"
	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const (
	SeverityInfo Severity = "info"
)

// Options contains the already-loaded runtime state used by doctor.
type Options struct {
	Paths         Paths
	Cache         *cache.Store
	Config        config.Config
	Executor      stratz.Executor
	SchemaVersion string
}

// Report is a deterministic, secret-free diagnostic result.
type Report struct {
	Findings []Finding
	Health   stratz.Health
}

// Diagnose validates local runtime state and performs one bounded upstream
// connectivity probe.
func Diagnose(ctx context.Context, options Options) Report {
	report := Report{
		Findings: append([]Finding(nil), CheckPermissions(options.Paths)...),
		Health:   stratz.Probe(ctx, options.Executor),
	}
	report.Findings = append(report.Findings, connectivityFinding(report.Health))

	schemaVersion := strings.TrimSpace(options.SchemaVersion)
	if schemaVersion == "" || schemaVersion == "unavailable" {
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityWarning,
			Code:     "schema_unavailable",
			Subject:  "schema",
			Message:  "no generated STRATZ schema snapshot is available",
		})
	} else {
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityInfo,
			Code:     "schema_available",
			Subject:  "schema",
			Message:  "generated schema metadata is available",
		})
	}

	switch cacheStatus(options.Cache, options.Config) {
	case cache.StatusHealthy:
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityInfo,
			Code:     "cache_healthy",
			Subject:  "cache",
			Message:  "cache backend is healthy",
		})
	case cache.StatusDegraded:
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityWarning,
			Code:     "cache_degraded",
			Subject:  "cache",
			Message:  "cache backend is degraded for this process",
		})
	default:
		report.Findings = append(report.Findings, Finding{
			Severity: SeverityInfo,
			Code:     "cache_disabled",
			Subject:  "cache",
			Message:  "cache is disabled",
		})
	}

	return report
}

func cacheStatus(store *cache.Store, cfg config.Config) cache.Status {
	if !cfg.Cache.Enabled {
		return cache.StatusDisabled
	}
	if store == nil {
		return cache.StatusDegraded
	}
	return store.Status()
}

// HasErrors reports whether doctor found a startup-blocking condition.
func (report Report) HasErrors() bool {
	for _, finding := range report.Findings {
		if finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func connectivityFinding(health stratz.Health) Finding {
	finding := Finding{
		Code:    "upstream_" + string(health.Status),
		Subject: "upstream",
	}
	switch health.Status {
	case stratz.HealthReachable:
		finding.Severity = SeverityInfo
		finding.Message = fmt.Sprintf(
			"STRATZ is reachable; %d rate-limit window(s) observed",
			len(health.RateLimits),
		)
	case stratz.HealthAuthenticationFailed:
		finding.Severity = SeverityError
		finding.Message = "STRATZ rejected the configured credential"
	case stratz.HealthWAFBlocked:
		finding.Severity = SeverityError
		finding.Message = "a Cloudflare challenge blocked STRATZ access"
	case stratz.HealthUnchecked:
		finding.Severity = SeverityWarning
		finding.Message = "STRATZ connectivity was not checked"
	default:
		finding.Severity = SeverityError
		finding.Message = "STRATZ is unreachable"
	}
	return finding
}
