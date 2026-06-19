package releasegate

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const SchemaVersion = 1

//go:embed current.json
var currentRecordJSON []byte

var RequiredDecisionIDs = []string{
	"api_wrapping",
	"local_caching",
	"schema_redistribution",
	"constants_redistribution",
	"attribution",
	"branding",
}

type Record struct {
	SchemaVersion int           `json:"schema_version"`
	ReviewedAt    string        `json:"reviewed_at"`
	PublicRelease PublicRelease `json:"public_release"`
	Decisions     []Decision    `json:"decisions"`
}

type PublicRelease struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

type Decision struct {
	ID         string   `json:"id"`
	Required   bool     `json:"required"`
	Status     string   `json:"status"`
	ReviewedAt string   `json:"reviewed_at"`
	Decision   string   `json:"decision"`
	SourceRefs []string `json:"source_refs"`
}

func Load(path string) (Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	return decode(file)
}

// Current returns the release-clearance record embedded in the binary.
func Current() (Record, error) {
	return decode(bytes.NewReader(currentRecordJSON))
}

func decode(reader io.Reader) (Record, error) {
	var record Record
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Record{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Record{}, errors.New("unexpected trailing JSON value")
		}
		return Record{}, err
	}
	return record, nil
}

func Validate(record Record) error {
	if record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported release-clearance schema version %d", record.SchemaVersion)
	}
	if _, err := time.Parse(time.DateOnly, record.ReviewedAt); err != nil {
		return fmt.Errorf("invalid reviewed_at: %w", err)
	}
	if strings.TrimSpace(record.PublicRelease.Reason) == "" {
		return errors.New("public_release.reason is required")
	}

	required := make(map[string]struct{}, len(RequiredDecisionIDs))
	for _, id := range RequiredDecisionIDs {
		required[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(record.Decisions))
	for _, decision := range record.Decisions {
		if _, ok := required[decision.ID]; !ok {
			return fmt.Errorf("unknown clearance decision %q", decision.ID)
		}
		if _, ok := seen[decision.ID]; ok {
			return fmt.Errorf("duplicate clearance decision %q", decision.ID)
		}
		seen[decision.ID] = struct{}{}
		if !decision.Required {
			return fmt.Errorf("clearance decision %q must remain required", decision.ID)
		}
		switch decision.Status {
		case "approved", "denied", "pending":
		default:
			return fmt.Errorf("clearance decision %q has invalid status %q", decision.ID, decision.Status)
		}
		if _, err := time.Parse(time.DateOnly, decision.ReviewedAt); err != nil {
			return fmt.Errorf("clearance decision %q has invalid reviewed_at: %w", decision.ID, err)
		}
		if strings.TrimSpace(decision.Decision) == "" {
			return fmt.Errorf("clearance decision %q must document release behavior", decision.ID)
		}
		if len(decision.SourceRefs) == 0 {
			return fmt.Errorf("clearance decision %q must include source references", decision.ID)
		}
		for _, source := range decision.SourceRefs {
			if strings.TrimSpace(source) == "" {
				return fmt.Errorf("clearance decision %q contains an empty source reference", decision.ID)
			}
		}
	}
	for _, id := range RequiredDecisionIDs {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("missing clearance decision %q", id)
		}
	}
	return nil
}

func Check(record Record) error {
	if err := Validate(record); err != nil {
		return err
	}
	for _, decision := range record.Decisions {
		if decision.Required && decision.Status != "approved" {
			return fmt.Errorf("public release blocked: %s clearance is %s", decision.ID, decision.Status)
		}
	}
	if !record.PublicRelease.Allowed {
		return errors.New("public release blocked by public_release.allowed=false")
	}
	return nil
}
