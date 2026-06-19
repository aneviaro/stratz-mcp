package discoveryevidence

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepositoryEvidenceIsComplete(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	record, err := Load(filepath.Join(root, "docs", "discovery-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(record, root); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyFixturesMatchRecordedOutcomes(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	record, err := Load(filepath.Join(root, "docs", "discovery-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, probe := range record.Probes {
		if probe.EvidenceType != "mock_policy" {
			continue
		}
		probe := probe
		t.Run(probe.ID, func(t *testing.T) {
			fixture, err := LoadFixture(filepath.Join(root, filepath.FromSlash(probe.Artifact)))
			if err != nil {
				t.Fatal(err)
			}
			if fixture.ID != probe.ID {
				t.Fatalf("fixture ID %q does not match probe ID %q", fixture.ID, probe.ID)
			}
			actual, err := EvaluateFixture(fixture)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, fixture.Expected) {
				t.Fatalf("outcome mismatch: got %+v, want %+v", actual, fixture.Expected)
			}
		})
	}
}
