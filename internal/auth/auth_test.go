package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCredentialSources(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		options LoadOptions
		want    Credential
		wantErr string
	}{
		{
			name:    "environment",
			options: LoadOptions{Environment: map[string]string{"STRATZ_API_TOKEN": "environment-token"}},
			want:    Credential{Token: "environment-token", Source: SourceEnvironment},
		},
		{
			name:    "file",
			options: LoadOptions{Environment: map[string]string{}, TokenFile: tokenPath},
			want:    Credential{Token: "file-token", Source: SourceFile},
		},
		{
			name: "conflicting sources",
			options: LoadOptions{
				Environment: map[string]string{"STRATZ_API_TOKEN": "environment-token"},
				TokenFile:   tokenPath,
			},
			wantErr: "exactly one",
		},
		{
			name:    "absent",
			options: LoadOptions{Environment: map[string]string{}},
			wantErr: "absent",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Load(test.options)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, test.wantErr)
				}
				if err != nil && strings.Contains(err.Error(), "environment-token") {
					t.Fatalf("error leaked token: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("credential = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestLoadRejectsUnsafeTokenFiles(t *testing.T) {
	directory := t.TempDir()
	tests := []struct {
		name    string
		content []byte
		wantErr string
	}{
		{"empty", nil, "empty"},
		{"multiline", []byte("first\nsecond\n"), "one line"},
		{"nul", []byte("token\x00value"), "NUL"},
		{"oversized", []byte(strings.Repeat("x", MaxTokenFileBytes+1)), "16 KiB"},
		{"leading whitespace", []byte(" token"), "whitespace"},
		{"two newlines", []byte("token\n\n"), "one line"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(directory, strings.ReplaceAll(test.name, " ", "-"))
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(LoadOptions{Environment: map[string]string{}, TokenFile: path})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadRejectsTokenFileSymlinkAndDirectory(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "token")
	if err := os.WriteFile(target, []byte("fixture-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "token-link")
	if err := os.Symlink(target, link); err == nil {
		_, loadErr := Load(LoadOptions{Environment: map[string]string{}, TokenFile: link})
		if loadErr == nil || !strings.Contains(loadErr.Error(), "symlink") {
			t.Fatalf("symlink error = %v", loadErr)
		}
	}

	_, err := Load(LoadOptions{Environment: map[string]string{}, TokenFile: directory})
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
}

func TestLoadAcceptsOptionalNewlineForms(t *testing.T) {
	for _, suffix := range []string{"", "\n", "\r\n"} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(suffix, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte("fixture-token"+suffix), 0o600); err != nil {
				t.Fatal(err)
			}
			credential, err := Load(LoadOptions{Environment: map[string]string{}, TokenFile: path})
			if err != nil {
				t.Fatal(err)
			}
			if credential.Token != "fixture-token" {
				t.Fatalf("token = %q", credential.Token)
			}
		})
	}
}
