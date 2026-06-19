package observability

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/config"
)

func TestRedactHeadersCoversSensitiveSet(t *testing.T) {
	redactor := NewRedactor("known-secret")
	headers := make(http.Header)
	required := []string{
		"Authorization",
		"Proxy-Authorization",
		"Cookie",
		"Set-Cookie",
		"WWW-Authenticate",
		"Authentication-Info",
		"Proxy-Authentication-Info",
		"X-API-Key",
		"API-Key",
		"X-Auth-Token",
		"X-SteamId",
		"X-SteamId-Ok",
	}
	for _, name := range required {
		if !IsSensitiveHeader(name) {
			t.Fatalf("%s is missing from the sensitive-header set", name)
		}
		headers.Set(name, "known-secret-or-sensitive")
	}
	headers.Set("Content-Type", "application/known-secret")

	safe := redactor.RedactHeaders(headers)
	for _, name := range required {
		if got := safe.Get(name); got != Redacted {
			t.Fatalf("%s = %q, want redacted", name, got)
		}
	}
	if strings.Contains(safe.Get("Content-Type"), "known-secret") {
		t.Fatal("known secret remained in non-sensitive header")
	}
}

func TestLoggerRedactsStructuredAndFreeFormSecrets(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := Logger(&output, config.LoggingConfig{
				Level:  "debug",
				Format: format,
			}, "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			logger.Error(
				"request failed for fixture-token",
				slog.String("authorization", "Bearer fixture-token"),
				slog.Any("error", errors.New("upstream echoed fixture-token")),
				slog.Any("headers", http.Header{
					"X-SteamId":    []string{"12345"},
					"Content-Type": []string{"fixture-token/json"},
				}),
				slog.Group("credentials", slog.String("token", "fixture-token")),
			)
			got := output.String()
			if strings.Contains(got, "fixture-token") || strings.Contains(got, "12345") {
				t.Fatalf("log leaked sensitive data: %s", got)
			}
			if !strings.Contains(got, Redacted) {
				t.Fatalf("log did not contain redaction marker: %s", got)
			}
		})
	}
}

func TestDefaultLoggerSuppressesLowerLevels(t *testing.T) {
	var output bytes.Buffer
	logger, err := Logger(&output, config.Defaults(t.TempDir()).Logging)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("not emitted")
	if output.Len() != 0 {
		t.Fatalf("default logger emitted info: %q", output.String())
	}
}
