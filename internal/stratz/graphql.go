package stratz

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

type graphqlEnvelope struct {
	data          json.RawMessage
	errors        json.RawMessage
	extensions    json.RawMessage
	graphqlErrors []GraphQLError
	hasData       bool
}

func decodeGraphQL(body []byte) (graphqlEnvelope, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&raw); err != nil {
		return graphqlEnvelope{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return graphqlEnvelope{}, errors.New("multiple JSON values")
		}
		return graphqlEnvelope{}, err
	}

	data, dataExists := raw["data"]
	errorsRaw, errorsExist := raw["errors"]
	extensions := raw["extensions"]
	if !dataExists && !errorsExist {
		return graphqlEnvelope{}, errors.New("missing data and errors")
	}

	var graphqlErrors []GraphQLError
	if errorsExist && !bytes.Equal(bytes.TrimSpace(errorsRaw), []byte("null")) {
		if err := json.Unmarshal(errorsRaw, &graphqlErrors); err != nil {
			return graphqlEnvelope{}, errors.New("invalid errors value")
		}
	}
	hasData := dataExists && !bytes.Equal(bytes.TrimSpace(data), []byte("null"))
	return graphqlEnvelope{
		data:          cloneRaw(data),
		errors:        cloneRaw(errorsRaw),
		extensions:    cloneRaw(extensions),
		graphqlErrors: graphqlErrors,
		hasData:       hasData,
	}, nil
}

func classifyGraphQLError(
	status int,
	envelope graphqlEnvelope,
	mode Mode,
	rates []RateLimit,
	requestID string,
) *Error {
	codes := graphqlCodes(envelope.graphqlErrors)
	details := map[string]any{
		"http_status": status,
	}
	if len(codes) > 0 {
		details["graphql_codes"] = codes
	}
	if requestID != "" {
		details["request_id"] = requestID
	}

	if containsCode(codes, "PRIVATE", "PRIVATE_PROFILE", "FORBIDDEN_PRIVATE") {
		return &Error{
			Code:       contracts.ErrorCodePrivate,
			Message:    "The requested STRATZ profile or data is private",
			Details:    details,
			RateLimits: rates,
			Retryable:  false,
		}
	}
	if containsCode(codes, "UNAUTHENTICATED", "AUTHENTICATION_ERROR", "TOKEN_EXPIRED") {
		return &Error{
			Code:       contracts.ErrorCodeAuthenticationFailed,
			Message:    "STRATZ rejected the configured credential",
			Details:    details,
			RateLimits: rates,
			Retryable:  false,
		}
	}
	if mode == ModeRaw && status == 400 {
		return &Error{
			Code:       contracts.ErrorCodeInvalidArgument,
			Message:    "STRATZ rejected the GraphQL input",
			Details:    details,
			RateLimits: rates,
			Retryable:  false,
		}
	}
	if envelope.hasData {
		return &Error{
			Code:       contracts.ErrorCodeUpstreamPartialError,
			Message:    "STRATZ returned partial data that cannot be normalized safely",
			Details:    details,
			RateLimits: rates,
			Retryable:  retryableGraphQLCodes(codes),
		}
	}
	return &Error{
		Code:       contracts.ErrorCodeUpstreamError,
		Message:    "STRATZ returned GraphQL execution errors",
		Details:    details,
		RateLimits: rates,
		Retryable:  retryableGraphQLCodes(codes),
	}
}

func graphqlCodes(graphqlErrors []GraphQLError) []string {
	seen := make(map[string]struct{})
	codes := make([]string, 0, len(graphqlErrors))
	for _, graphqlErr := range graphqlErrors {
		for _, key := range []string{"code", "codes"} {
			value, ok := graphqlErr.Extensions[key]
			if !ok {
				continue
			}
			for _, code := range stringValues(value) {
				code = sanitizeCode(code)
				if code == "" {
					continue
				}
				if _, exists := seen[code]; exists {
					continue
				}
				seen[code] = struct{}{}
				codes = append(codes, code)
				if len(codes) == 16 {
					return codes
				}
			}
		}
	}
	return codes
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func sanitizeCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return ""
		}
	}
	return value
}

func containsCode(codes []string, expected ...string) bool {
	for _, code := range codes {
		for _, candidate := range expected {
			if code == candidate {
				return true
			}
		}
	}
	return false
}

func retryableGraphQLCodes(codes []string) bool {
	return containsCode(codes, "INTERNAL_SERVER_ERROR", "SERVICE_UNAVAILABLE", "TIMEOUT")
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}
