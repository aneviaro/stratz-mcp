// Package pagination implements shared cursor and list-continuation helpers.
package pagination

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

const (
	// CursorVersion is the only accepted opaque cursor format version.
	CursorVersion = "v1"

	cursorHKDFInfo      = "stratz-mcp/cursor/v1"
	tokenNamespaceLabel = "stratz-mcp/token-namespace/v1"
)

// Lifetime selects the bounded expiry class for a list cursor.
type Lifetime string

const (
	LifetimeLive       Lifetime = "live"
	LifetimeRecent     Lifetime = "recent"
	LifetimeHistorical Lifetime = "historical"
)

// Binding captures the current request attributes that the cursor must match
// exactly on resume. Filters must exclude cursor and page-size values because
// those are bound separately.
type Binding struct {
	Tool             string
	Filters          any
	PageSize         int
	Token            string
	SchemaVersion    string
	OperationVersion string
}

// Payload is the canonical signed cursor body.
type Payload struct {
	Version          string          `json:"version"`
	Tool             string          `json:"tool"`
	FilterHash       string          `json:"filter_hash"`
	PageSize         int             `json:"page_size"`
	State            json.RawMessage `json:"state,omitempty"`
	TokenNamespace   string          `json:"token_namespace"`
	SchemaVersion    string          `json:"schema_version"`
	OperationVersion string          `json:"operation_version"`
	IssuedAt         time.Time       `json:"issued_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
}

// Error is a stable cursor validation failure suitable for later MCP error
// mapping.
type Error struct {
	Code    contracts.ErrorCode
	Message string
	Details map[string]any
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// Options configures deterministic cursor encoding and validation.
type Options struct {
	Now func() time.Time
}

// Codec signs and validates opaque list cursors.
type Codec struct {
	now func() time.Time
}

// NewCodec constructs a cursor codec with bounded defaults.
func NewCodec(options Options) *Codec {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Codec{now: now}
}

// Encode signs one cursor payload for the supplied binding and lifetime class.
func (codec *Codec) Encode(binding Binding, lifetime Lifetime, state any) (string, error) {
	binding, err := normalizeBinding(binding)
	if err != nil {
		return "", err
	}
	duration, err := lifetime.Duration()
	if err != nil {
		return "", err
	}
	filterHash, err := FilterHash(binding.Filters)
	if err != nil {
		return "", fmt.Errorf("hash cursor filters: %w", err)
	}
	namespace, err := tokenNamespace(binding.Token)
	if err != nil {
		return "", err
	}
	now := codec.now().UTC().Truncate(time.Second)
	payload := Payload{
		Version:          CursorVersion,
		Tool:             binding.Tool,
		FilterHash:       filterHash,
		PageSize:         binding.PageSize,
		TokenNamespace:   namespace,
		SchemaVersion:    binding.SchemaVersion,
		OperationVersion: binding.OperationVersion,
		IssuedAt:         now,
		ExpiresAt:        now.Add(duration),
	}
	if state != nil {
		stateBytes, err := canonicalJSON(state)
		if err != nil {
			return "", fmt.Errorf("encode cursor state: %w", err)
		}
		payload.State = stateBytes
	}
	payloadBytes, err := canonicalJSON(payload)
	if err != nil {
		return "", fmt.Errorf("encode cursor payload: %w", err)
	}
	payloadToken := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature, err := sign(binding.Token, payloadToken)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		CursorVersion,
		payloadToken,
		base64.RawURLEncoding.EncodeToString(signature),
	}, "."), nil
}

// Decode verifies one opaque cursor, validates its binding, and optionally
// decodes the bound continuation state into state.
func (codec *Codec) Decode(cursor string, binding Binding, state any) (*Payload, error) {
	binding, err := normalizeBinding(binding)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(cursor, ".")
	if len(parts) != 3 || parts[0] != CursorVersion {
		return nil, invalidCursor("The pagination cursor is malformed", nil)
	}

	payloadToken := parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, invalidCursor("The pagination cursor signature is malformed", nil)
	}
	expectedSignature, err := sign(binding.Token, payloadToken)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(signature, expectedSignature) {
		return nil, invalidCursor("The pagination cursor signature is invalid", nil)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadToken)
	if err != nil {
		return nil, invalidCursor("The pagination cursor payload is malformed", nil)
	}
	var payload Payload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, invalidCursor("The pagination cursor payload is invalid JSON", nil)
	}
	if err := validatePayload(payload); err != nil {
		return nil, invalidCursor(err.Error(), nil)
	}
	filterHash, err := FilterHash(binding.Filters)
	if err != nil {
		return nil, fmt.Errorf("hash cursor filters: %w", err)
	}
	namespace, err := tokenNamespace(binding.Token)
	if err != nil {
		return nil, err
	}
	switch {
	case payload.Version != CursorVersion:
		return nil, invalidCursor("The pagination cursor version is not supported", nil)
	case payload.Tool != binding.Tool:
		return nil, invalidCursor("The pagination cursor was created for a different tool", nil)
	case payload.FilterHash != filterHash:
		return nil, invalidCursor("The pagination cursor does not match the current filters", nil)
	case payload.PageSize != binding.PageSize:
		return nil, invalidCursor("The pagination cursor does not match the current page size", nil)
	case payload.TokenNamespace != namespace:
		return nil, invalidCursor("The pagination cursor does not match the active token", nil)
	case payload.SchemaVersion != binding.SchemaVersion:
		return nil, invalidCursor("The pagination cursor schema version is no longer valid", nil)
	case payload.OperationVersion != binding.OperationVersion:
		return nil, invalidCursor("The pagination cursor operation version is no longer valid", nil)
	}

	now := codec.now().UTC()
	if !now.Before(payload.ExpiresAt) {
		return nil, &Error{
			Code:    contracts.ErrorCodeCursorExpired,
			Message: "The pagination cursor has expired",
			Details: map[string]any{},
		}
	}
	if state != nil && len(payload.State) > 0 {
		if err := json.Unmarshal(payload.State, state); err != nil {
			return nil, invalidCursor("The pagination cursor state is invalid", nil)
		}
	}
	return &payload, nil
}

// Duration returns the architecture-defined lifetime for a cursor class.
func (lifetime Lifetime) Duration() (time.Duration, error) {
	switch lifetime {
	case LifetimeLive:
		return 5 * time.Minute, nil
	case LifetimeRecent:
		return time.Hour, nil
	case LifetimeHistorical:
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported cursor lifetime %q", lifetime)
	}
}

// FilterHash returns the canonical stable hash for the supplied filter object.
func FilterHash(filters any) (string, error) {
	if filters == nil {
		filters = map[string]any{}
	}
	data, err := canonicalJSON(filters)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeBinding(binding Binding) (Binding, error) {
	binding.Tool = strings.TrimSpace(binding.Tool)
	binding.SchemaVersion = strings.TrimSpace(binding.SchemaVersion)
	binding.OperationVersion = strings.TrimSpace(binding.OperationVersion)
	switch {
	case binding.Tool == "":
		return Binding{}, errors.New("cursor tool is required")
	case binding.PageSize < 1:
		return Binding{}, errors.New("cursor page size must be positive")
	case binding.SchemaVersion == "":
		return Binding{}, errors.New("cursor schema version is required")
	case binding.OperationVersion == "":
		return Binding{}, errors.New("cursor operation version is required")
	case binding.Token == "":
		return Binding{}, errors.New("cursor token is required")
	case hasControl(binding.Token):
		return Binding{}, errors.New("cursor token contains a prohibited control character")
	}
	return binding, nil
}

func validatePayload(payload Payload) error {
	switch {
	case payload.Tool == "":
		return errors.New("The pagination cursor tool is missing")
	case payload.FilterHash == "":
		return errors.New("The pagination cursor filter hash is missing")
	case payload.PageSize < 1:
		return errors.New("The pagination cursor page size is invalid")
	case payload.TokenNamespace == "":
		return errors.New("The pagination cursor token namespace is missing")
	case payload.SchemaVersion == "":
		return errors.New("The pagination cursor schema version is missing")
	case payload.OperationVersion == "":
		return errors.New("The pagination cursor operation version is missing")
	case payload.IssuedAt.IsZero() || payload.ExpiresAt.IsZero():
		return errors.New("The pagination cursor timestamps are invalid")
	case !payload.ExpiresAt.After(payload.IssuedAt):
		return errors.New("The pagination cursor expiry is invalid")
	}
	return nil
}

func sign(token, payloadToken string) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, []byte(token), nil, cursorHKDFInfo, sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("derive cursor signing key: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(CursorVersion))
	mac.Write([]byte{'.'})
	mac.Write([]byte(payloadToken))
	return mac.Sum(nil), nil
}

func tokenNamespace(token string) (string, error) {
	if token == "" {
		return "", errors.New("cursor token is required")
	}
	sum := sha256.Sum256([]byte(tokenNamespaceLabel + "\x00" + token))
	return base64.RawURLEncoding.EncodeToString(sum[:16]), nil
}

func canonicalJSON(value any) ([]byte, error) {
	normalized, err := normalizeJSON(value)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := writeCanonicalJSON(&buffer, normalized); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func normalizeJSON(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected trailing JSON value")
		}
		return nil, err
	}
	return normalized, nil
}

func writeCanonicalJSON(buffer *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		buffer.WriteString("null")
	case bool:
		if value {
			buffer.WriteString("true")
		} else {
			buffer.WriteString("false")
		}
	case string:
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		buffer.Write(encoded)
	case json.Number:
		if !validJSONNumber(value.String()) {
			return fmt.Errorf("invalid JSON number %q", value.String())
		}
		buffer.WriteString(value.String())
	case []any:
		buffer.WriteByte('[')
		for index, item := range value {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonicalJSON(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			encoded, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buffer.Write(encoded)
			buffer.WriteByte(':')
			if err := writeCanonicalJSON(buffer, value[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}

func validJSONNumber(value string) bool {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return false
	}
	if _, ok := decoded.(json.Number); !ok {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func hasControl(value string) bool {
	for _, character := range value {
		if character == '\r' || character == '\n' || character == 0 {
			return true
		}
	}
	return false
}

func invalidCursor(message string, details map[string]any) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{
		Code:    contracts.ErrorCodeCursorInvalid,
		Message: message,
		Details: details,
	}
}
