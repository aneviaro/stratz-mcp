package securefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
}

func TestOpenReadOnlyRejectsSymlinkAndDirectory(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		if _, openErr := OpenReadOnly(link); openErr == nil || !strings.Contains(openErr.Error(), "symlink") {
			t.Fatalf("symlink error = %v", openErr)
		}
	}
	if _, err := OpenReadOnly(directory); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
}
