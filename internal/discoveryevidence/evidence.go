package discoveryevidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	SchemaVersion       = 1
	MaxDecodedBodyBytes = 5 << 20
)

var RequiredProbeIDs = []string{
	"minimal_named_query",
	"minimal_gzip",
	"without_accept",
	"without_content_type",
	"without_user_agent",
	"invalid_token_malformed",
	"invalid_token_signature_corrupted",
	"missing_token",
	"malformed_json",
	"invalid_graphql_syntax",
	"invalid_graphql_field",
	"nullable_or_missing_data",
	"missing_match",
	"private_profile",
	"introspection",
	"rate_headers",
	"near_limit",
	"oversized_response",
	"client_timeout",
	"rate_limited_429",
	"expired_token",
}

type Record struct {
	SchemaVersion int     `json:"schema_version"`
	UpdatedAt     string  `json:"updated_at"`
	Probes        []Probe `json:"probes"`
}

type Probe struct {
	ID           string    `json:"id"`
	Purpose      string    `json:"purpose"`
	EvidenceType string    `json:"evidence_type"`
	ObservedAt   string    `json:"observed_at"`
	Artifact     string    `json:"artifact,omitempty"`
	Summary      string    `json:"summary"`
	Mappings     []Mapping `json:"mappings"`
}

type Mapping struct {
	Context   string `json:"context"`
	Outcome   string `json:"outcome"`
	MCPError  string `json:"mcp_error,omitempty"`
	Retryable bool   `json:"retryable"`
}

type PolicyFixture struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	HTTPStatus      int            `json:"http_status,omitempty"`
	GraphQLCode     string         `json:"graphql_code,omitempty"`
	HasData         bool           `json:"has_data,omitempty"`
	HasErrors       bool           `json:"has_errors,omitempty"`
	DecodedBytes    int            `json:"decoded_bytes,omitempty"`
	TransportError  string         `json:"transport_error,omitempty"`
	CredentialState string         `json:"credential_state,omitempty"`
	Expected        FixtureOutcome `json:"expected"`
	Notes           string         `json:"notes"`
}

type FixtureOutcome struct {
	Curated   string `json:"curated"`
	Raw       string `json:"raw"`
	Retryable bool   `json:"retryable"`
}

func Load(path string) (Record, error) {
	var record Record
	if err := decodeStrictFile(path, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func LoadFixture(path string) (PolicyFixture, error) {
	var fixture PolicyFixture
	if err := decodeStrictFile(path, &fixture); err != nil {
		return PolicyFixture{}, err
	}
	return fixture, nil
}

func Validate(record Record, repositoryRoot string) error {
	if record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported discovery evidence schema version %d", record.SchemaVersion)
	}
	if _, err := time.Parse(time.DateOnly, record.UpdatedAt); err != nil {
		return fmt.Errorf("invalid updated_at: %w", err)
	}

	required := make(map[string]struct{}, len(RequiredProbeIDs))
	for _, id := range RequiredProbeIDs {
		required[id] = struct{}{}
	}

	seen := make(map[string]struct{}, len(record.Probes))
	for _, probe := range record.Probes {
		if _, ok := required[probe.ID]; !ok {
			return fmt.Errorf("unknown discovery probe %q", probe.ID)
		}
		if _, ok := seen[probe.ID]; ok {
			return fmt.Errorf("duplicate discovery probe %q", probe.ID)
		}
		seen[probe.ID] = struct{}{}

		switch probe.EvidenceType {
		case "live", "mock_policy", "documentary_policy":
		default:
			return fmt.Errorf("probe %q has invalid evidence_type %q", probe.ID, probe.EvidenceType)
		}
		if _, err := time.Parse(time.DateOnly, probe.ObservedAt); err != nil {
			return fmt.Errorf("probe %q has invalid observed_at: %w", probe.ID, err)
		}
		if strings.TrimSpace(probe.Purpose) == "" || strings.TrimSpace(probe.Summary) == "" {
			return fmt.Errorf("probe %q must include purpose and summary", probe.ID)
		}
		if len(probe.Mappings) == 0 {
			return fmt.Errorf("probe %q must include at least one mapping", probe.ID)
		}
		for _, mapping := range probe.Mappings {
			if strings.TrimSpace(mapping.Context) == "" || strings.TrimSpace(mapping.Outcome) == "" {
				return fmt.Errorf("probe %q contains an incomplete mapping", probe.ID)
			}
			if mapping.Outcome == "error" && strings.TrimSpace(mapping.MCPError) == "" {
				return fmt.Errorf("probe %q error mapping must include mcp_error", probe.ID)
			}
		}
		if probe.EvidenceType == "mock_policy" {
			if probe.Artifact == "" {
				return fmt.Errorf("mock-policy probe %q must reference an artifact", probe.ID)
			}
			if _, err := os.Stat(filepath.Join(repositoryRoot, filepath.FromSlash(probe.Artifact))); err != nil {
				return fmt.Errorf("probe %q artifact: %w", probe.ID, err)
			}
		}
	}

	for _, id := range RequiredProbeIDs {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("missing discovery evidence for %q", id)
		}
	}
	return nil
}

func EvaluateFixture(fixture PolicyFixture) (FixtureOutcome, error) {
	var actual FixtureOutcome
	switch {
	case fixture.CredentialState == "expired":
		actual = FixtureOutcome{
			Curated: "AUTHENTICATION_FAILED",
			Raw:     "AUTHENTICATION_FAILED",
		}
	case fixture.TransportError == "context_deadline_exceeded":
		actual = FixtureOutcome{
			Curated:   "UPSTREAM_TIMEOUT",
			Raw:       "UPSTREAM_TIMEOUT",
			Retryable: true,
		}
	case fixture.DecodedBytes > MaxDecodedBodyBytes:
		actual = FixtureOutcome{
			Curated: "RESPONSE_TOO_LARGE",
			Raw:     "RESPONSE_TOO_LARGE",
		}
	case fixture.HTTPStatus == 429:
		actual = FixtureOutcome{
			Curated:   "RATE_LIMITED",
			Raw:       "RATE_LIMITED",
			Retryable: true,
		}
	case fixture.GraphQLCode == "PRIVATE_PROFILE":
		actual = FixtureOutcome{
			Curated: "PRIVATE",
			Raw:     "partial_success",
		}
	case fixture.HasData && fixture.HasErrors:
		actual = FixtureOutcome{
			Curated: "UPSTREAM_PARTIAL_ERROR",
			Raw:     "partial_success",
		}
	default:
		return FixtureOutcome{}, fmt.Errorf("fixture %q does not match a discovery policy", fixture.ID)
	}
	return actual, nil
}

func decodeStrictFile(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
