// Package observability provides local stderr logging with centralized
// redaction and no telemetry.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/config"
)

const Redacted = "[REDACTED]"

var sensitiveHeaders = map[string]struct{}{
	"api-key":                   {},
	"authentication-info":       {},
	"authorization":             {},
	"cookie":                    {},
	"proxy-authentication-info": {},
	"proxy-authorization":       {},
	"set-cookie":                {},
	"www-authenticate":          {},
	"x-api-key":                 {},
	"x-auth-token":              {},
	"x-steamid":                 {},
	"x-steamid-ok":              {},
}

var sensitiveKeys = map[string]struct{}{
	"access_token":     {},
	"api_token":        {},
	"client_secret":    {},
	"dotenv":           {},
	"password":         {},
	"refresh_token":    {},
	"secret":           {},
	"stratz_api_token": {},
	"token":            {},
}

// Redactor removes sensitive attributes, headers, and known secret values.
type Redactor struct {
	secrets []string
}

func NewRedactor(secrets ...string) Redactor {
	filtered := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret != "" && secret != Redacted {
			filtered = append(filtered, secret)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return len(filtered[i]) > len(filtered[j])
	})
	return Redactor{secrets: filtered}
}

// Logger creates a text or JSON slog logger suitable for stderr.
func Logger(writer io.Writer, logging config.LoggingConfig, secrets ...string) (*slog.Logger, error) {
	level, err := parseLevel(logging.Level)
	if err != nil {
		return nil, err
	}
	redactor := NewRedactor(secrets...)
	options := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactor.ReplaceAttr,
	}

	var handler slog.Handler
	switch logging.Format {
	case "text":
		handler = slog.NewTextHandler(writer, options)
	case "json":
		handler = slog.NewJSONHandler(writer, options)
	default:
		return nil, fmt.Errorf("unsupported log format %q", logging.Format)
	}
	return slog.New(handler), nil
}

// ReplaceAttr is suitable for slog.HandlerOptions.ReplaceAttr.
func (redactor Redactor) ReplaceAttr(_ []string, attribute slog.Attr) slog.Attr {
	if isSensitiveKey(attribute.Key) {
		return slog.String(attribute.Key, Redacted)
	}
	return slog.Attr{
		Key:   attribute.Key,
		Value: redactor.redactValue(attribute.Value),
	}
}

// RedactString removes every configured secret from free-form text.
func (redactor Redactor) RedactString(value string) string {
	for _, secret := range redactor.secrets {
		value = strings.ReplaceAll(value, secret, Redacted)
	}
	return value
}

// RedactHeaders returns a copy with every sensitive header value removed and
// every other value scrubbed for known secrets.
func (redactor Redactor) RedactHeaders(headers http.Header) http.Header {
	result := make(http.Header, len(headers))
	for name, values := range headers {
		if IsSensitiveHeader(name) {
			result[name] = []string{Redacted}
			continue
		}
		safeValues := make([]string, len(values))
		for index, value := range values {
			safeValues[index] = redactor.RedactString(value)
		}
		result[name] = safeValues
	}
	return result
}

// IsSensitiveHeader reports whether a header must never be logged.
func IsSensitiveHeader(name string) bool {
	_, sensitive := sensitiveHeaders[strings.ToLower(strings.TrimSpace(name))]
	return sensitive
}

// SensitiveHeaderNames returns the centrally maintained redaction set.
func SensitiveHeaderNames() []string {
	names := make([]string, 0, len(sensitiveHeaders))
	for name := range sensitiveHeaders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (redactor Redactor) redactValue(value slog.Value) slog.Value {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(redactor.RedactString(value.String()))
	case slog.KindAny:
		switch actual := value.Any().(type) {
		case error:
			return slog.StringValue(redactor.RedactString(actual.Error()))
		case http.Header:
			return slog.AnyValue(redactor.RedactHeaders(actual))
		case map[string]string:
			safe := make(map[string]string, len(actual))
			for key, item := range actual {
				if isSensitiveKey(key) {
					safe[key] = Redacted
				} else {
					safe[key] = redactor.RedactString(item)
				}
			}
			return slog.AnyValue(safe)
		case map[string][]string:
			return slog.AnyValue(redactor.redactHeaderMap(actual))
		case map[string]any:
			safe := make(map[string]any, len(actual))
			for key, item := range actual {
				if isSensitiveKey(key) {
					safe[key] = Redacted
				} else {
					safe[key] = redactor.redactAny(item)
				}
			}
			return slog.AnyValue(safe)
		default:
			return slog.AnyValue(redactor.redactAny(actual))
		}
	case slog.KindGroup:
		attributes := value.Group()
		for index := range attributes {
			attributes[index] = redactor.ReplaceAttr(nil, attributes[index])
		}
		return slog.GroupValue(attributes...)
	default:
		return value
	}
}

func (redactor Redactor) redactAny(value any) any {
	switch actual := value.(type) {
	case string:
		return redactor.RedactString(actual)
	case error:
		return redactor.RedactString(actual.Error())
	case []string:
		safe := make([]string, len(actual))
		for index, item := range actual {
			safe[index] = redactor.RedactString(item)
		}
		return safe
	case []any:
		safe := make([]any, len(actual))
		for index, item := range actual {
			safe[index] = redactor.redactAny(item)
		}
		return safe
	case map[string]string:
		safe := make(map[string]string, len(actual))
		for key, item := range actual {
			if isSensitiveKey(key) {
				safe[key] = Redacted
			} else {
				safe[key] = redactor.RedactString(item)
			}
		}
		return safe
	case map[string][]string:
		return redactor.redactHeaderMap(actual)
	case map[string]any:
		safe := make(map[string]any, len(actual))
		for key, item := range actual {
			if isSensitiveKey(key) {
				safe[key] = Redacted
			} else {
				safe[key] = redactor.redactAny(item)
			}
		}
		return safe
	default:
		return value
	}
}

func (redactor Redactor) redactHeaderMap(values map[string][]string) map[string][]string {
	safe := make(map[string][]string, len(values))
	for key, items := range values {
		if IsSensitiveHeader(key) || isSensitiveKey(key) {
			safe[key] = []string{Redacted}
			continue
		}
		safeItems := make([]string, len(items))
		for index, item := range items {
			safeItems[index] = redactor.RedactString(item)
		}
		safe[key] = safeItems
	}
	return safe
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if IsSensitiveHeader(key) {
		return true
	}
	if _, sensitive := sensitiveKeys[key]; sensitive {
		return true
	}
	segments := strings.FieldsFunc(key, func(character rune) bool {
		return character == '.' || character == '/' || character == ':'
	})
	for _, segment := range segments {
		if IsSensitiveHeader(segment) {
			return true
		}
		if _, sensitive := sensitiveKeys[segment]; sensitive {
			return true
		}
	}
	return false
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "error":
		return slog.LevelError, nil
	case "warn":
		return slog.LevelWarn, nil
	case "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", value)
	}
}
