package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckPermissionsAcceptsPrivatePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	configPath := filepath.Join(directory, "config.yaml")
	cacheDirectory := filepath.Join(directory, "cache")
	if err := os.WriteFile(tokenPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cacheDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDirectory, "cache.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	findings := CheckPermissions(Paths{
		TokenFile:      tokenPath,
		ConfigFile:     configPath,
		CacheDirectory: cacheDirectory,
	})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestCheckPermissionsReportsExposedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	configPath := filepath.Join(directory, "config.yaml")
	cacheDirectory := filepath.Join(directory, "cache")
	if err := os.WriteFile(tokenPath, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o622); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0o622); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cacheDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDirectory, "cache.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	findings := CheckPermissions(Paths{
		TokenFile:      tokenPath,
		ConfigFile:     configPath,
		CacheDirectory: cacheDirectory,
	})
	codes := map[string]bool{}
	for _, finding := range findings {
		codes[finding.Code] = true
	}
	for _, code := range []string{
		"secret_group_or_other_readable",
		"group_or_other_writable",
		"cache_directory_exposed",
		"cache_file_exposed",
	} {
		if !codes[code] {
			t.Fatalf("missing finding %q in %+v", code, findings)
		}
	}
}

func TestCheckPermissionsRejectsSymlinks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	findings := CheckPermissions(Paths{TokenFile: link})
	if len(findings) != 1 || findings[0].Code != "symlink_rejected" {
		t.Fatalf("findings = %+v", findings)
	}
}
