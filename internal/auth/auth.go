// Package auth loads and validates STRATZ credentials without exposing them
// through general configuration or diagnostics.
package auth

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/securefile"
)

// MaxTokenFileBytes bounds token-file reads before validation.
const MaxTokenFileBytes = 16 << 10

// Source identifies the non-sensitive origin of a credential.
type Source string

const (
	// SourceEnvironment identifies a token loaded from STRATZ_API_TOKEN.
	SourceEnvironment Source = "environment"
	// SourceFile identifies a token loaded from a file.
	SourceFile Source = "file"
)

// Credential contains the active token and its non-sensitive source type.
type Credential struct {
	Token  string
	Source Source
}

// LoadOptions configures credential loading.
type LoadOptions struct {
	Environment map[string]string
	TokenFile   string
}

// Load enforces exactly one effective credential source.
func Load(options LoadOptions) (Credential, error) {
	environmentToken := options.Environment["STRATZ_API_TOKEN"]
	hasEnvironmentToken := environmentToken != ""
	hasTokenFile := options.TokenFile != ""

	if hasEnvironmentToken && hasTokenFile {
		return Credential{}, errors.New("configure exactly one credential source: STRATZ_API_TOKEN or token file")
	}
	if !hasEnvironmentToken && !hasTokenFile {
		return Credential{}, errors.New("STRATZ API token is absent; set STRATZ_API_TOKEN or select a token file")
	}
	if hasEnvironmentToken {
		token, err := validateToken([]byte(environmentToken), false)
		if err != nil {
			return Credential{}, errors.New("STRATZ_API_TOKEN is invalid: " + err.Error())
		}
		return Credential{Token: token, Source: SourceEnvironment}, nil
	}

	token, err := readTokenFile(options.TokenFile)
	if err != nil {
		return Credential{}, errors.New("selected token file is invalid or unusable: " + err.Error())
	}
	return Credential{Token: token, Source: SourceFile}, nil
}

func readTokenFile(path string) (string, error) {
	file, err := securefile.OpenReadOnly(path)
	if err != nil {
		return "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxTokenFileBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		readError := errors.New("cannot read file")
		if closeErr != nil {
			return "", errors.Join(readError, fmt.Errorf("close token file: %w", closeErr))
		}
		return "", readError
	}
	if closeErr != nil {
		return "", fmt.Errorf("close token file: %w", closeErr)
	}
	if len(data) > MaxTokenFileBytes {
		return "", errors.New("file exceeds 16 KiB")
	}
	return validateToken(data, true)
}

func validateToken(data []byte, allowTrailingNewline bool) (string, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return "", errors.New("token contains a NUL byte")
	}
	if allowTrailingNewline {
		data = bytes.TrimSuffix(data, []byte("\r\n"))
		data = bytes.TrimSuffix(data, []byte("\n"))
	}
	if bytes.ContainsAny(data, "\r\n") {
		return "", errors.New("token must contain exactly one line")
	}
	if len(data) == 0 {
		return "", errors.New("token is empty")
	}
	token := string(data)
	if strings.TrimSpace(token) != token {
		return "", errors.New("token has leading or trailing whitespace")
	}
	if len(token) > MaxTokenFileBytes {
		return "", errors.New("token exceeds 16 KiB")
	}
	return token, nil
}
