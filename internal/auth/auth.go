// Package auth loads and validates STRATZ credentials without exposing them
// through general configuration or diagnostics.
package auth

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/securefile"
)

const MaxTokenFileBytes = 16 << 10

type Source string

const (
	SourceEnvironment Source = "environment"
	SourceFile        Source = "file"
)

// Credential contains the active token and its non-sensitive source type.
type Credential struct {
	Token  string
	Source Source
}

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
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxTokenFileBytes+1))
	if err != nil {
		return "", errors.New("cannot read file")
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
