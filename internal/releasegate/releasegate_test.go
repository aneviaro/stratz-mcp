package releasegate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryRecordBlocksPublishing(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "release-clearance.json")
	record, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(record); err != nil {
		t.Fatal(err)
	}
	if err := Check(record); err == nil {
		t.Fatal("publishing was allowed without complete STRATZ clearance")
	}
}

func TestCheckAllowsOnlyCompleteApproval(t *testing.T) {
	record := Record{
		SchemaVersion: SchemaVersion,
		ReviewedAt:    "2026-06-19",
		PublicRelease: PublicRelease{
			Allowed: true,
			Reason:  "All required permissions approved.",
		},
	}
	for _, id := range RequiredDecisionIDs {
		record.Decisions = append(record.Decisions, Decision{
			ID:         id,
			Required:   true,
			Status:     "approved",
			ReviewedAt: "2026-06-19",
			Decision:   "Approved for public release.",
			SourceRefs: []string{"https://example.invalid/written-clearance"},
		})
	}
	if err := Check(record); err != nil {
		t.Fatalf("complete approval was rejected: %v", err)
	}

	record.Decisions[0].Status = "pending"
	if err := Check(record); err == nil || !strings.Contains(err.Error(), record.Decisions[0].ID) {
		t.Fatalf("pending clearance was not rejected with its decision ID: %v", err)
	}
}
