package app

import "testing"

func TestBuildInfoNormalized(t *testing.T) {
	info := (BuildInfo{}).Normalized()
	if info.Version != DevelopmentVersion {
		t.Fatalf("version = %q, want %q", info.Version, DevelopmentVersion)
	}
	if info.Revision != UnknownRevision {
		t.Fatalf("revision = %q, want %q", info.Revision, UnknownRevision)
	}
	if info.SchemaVersion != UnknownSchema {
		t.Fatalf("schema version = %q, want %q", info.SchemaVersion, UnknownSchema)
	}
}

func TestBuildInfoString(t *testing.T) {
	info := BuildInfo{
		Version:       "v1.2.3",
		Revision:      "abc123",
		SchemaVersion: "sha256:fixture",
	}
	const want = "version=v1.2.3 revision=abc123 schema_version=sha256:fixture"
	if got := info.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
