package main

import "testing"

func TestBuildInfoUsesInjectedValues(t *testing.T) {
	originalVersion := version
	originalRevision := revision
	originalSchemaVersion := schemaVersion
	t.Cleanup(func() {
		version = originalVersion
		revision = originalRevision
		schemaVersion = originalSchemaVersion
	})

	version = "v1.2.3"
	revision = "abc123"
	schemaVersion = "sha256:fixture"

	info := buildInfo()
	if info.Version != version {
		t.Fatalf("version = %q, want %q", info.Version, version)
	}
	if info.Revision != revision {
		t.Fatalf("revision = %q, want %q", info.Revision, revision)
	}
	if info.SchemaVersion != schemaVersion {
		t.Fatalf("schema version = %q, want %q", info.SchemaVersion, schemaVersion)
	}
}
