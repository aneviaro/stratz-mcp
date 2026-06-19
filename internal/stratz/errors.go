package stratz

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func classifyTransportError(ctx context.Context, err error) *Error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return classifyContextError(ctxErr)
	}
	if isTLSError(err) {
		return &Error{
			Code:      contracts.ErrorCodeUpstreamTLSError,
			Message:   "TLS validation for STRATZ failed",
			Details:   map[string]any{},
			Retryable: false,
			cause:     err,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return classifyContextError(context.DeadlineExceeded)
	}
	retryable := isTemporaryNetworkError(err)
	return &Error{
		Code:      contracts.ErrorCodeUpstreamNetworkError,
		Message:   "The STRATZ network request failed",
		Details:   map[string]any{},
		Retryable: retryable,
		cause:     err,
	}
}

func classifyContextError(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{
			Code:      contracts.ErrorCodeUpstreamTimeout,
			Message:   "The STRATZ request timed out",
			Details:   map[string]any{},
			Retryable: true,
			cause:     context.DeadlineExceeded,
		}
	}
	return &Error{
		Code:      contracts.ErrorCodeUpstreamNetworkError,
		Message:   "The STRATZ request was canceled",
		Details:   map[string]any{},
		Retryable: false,
		cause:     context.Canceled,
	}
}

func classifyHTTPError(
	response *http.Response,
	body []byte,
	mode Mode,
	rates []RateLimit,
	requestID string,
	now time.Time,
) *Error {
	details := responseDetails(response, requestID)
	retryAfter := retryAt(response.Header, rates, now)
	envelope, graphQLErr := decodeGraphQL(body)
	hasGraphQL := graphQLErr == nil && len(envelope.graphqlErrors) > 0

	switch response.StatusCode {
	case http.StatusUnauthorized:
		return withRateLimits(authenticationError(details), rates)
	case http.StatusForbidden:
		if hasGraphQL {
			graphErr := classifyGraphQLError(response.StatusCode, envelope, mode, rates, requestID)
			if graphErr.Code == contracts.ErrorCodePrivate {
				return graphErr
			}
		}
		return withRateLimits(authenticationError(details), rates)
	case http.StatusRequestTimeout:
		return &Error{
			Code:       contracts.ErrorCodeUpstreamTimeout,
			Message:    "STRATZ timed out while processing the request",
			Details:    details,
			RateLimits: rates,
			Retryable:  true,
		}
	case http.StatusRequestEntityTooLarge:
		return &Error{
			Code:       contracts.ErrorCodeResponseTooLarge,
			Message:    "STRATZ rejected the request or response as too large",
			Details:    details,
			RateLimits: rates,
			Retryable:  false,
		}
	case http.StatusTooManyRequests:
		return &Error{
			Code:       contracts.ErrorCodeRateLimited,
			Message:    "STRATZ rate-limited the request",
			Details:    details,
			RateLimits: rates,
			Retryable:  true,
			RetryAfter: retryAfter,
		}
	case http.StatusBadRequest:
		if hasGraphQL && mode == ModeRaw {
			return classifyGraphQLError(response.StatusCode, envelope, mode, rates, requestID)
		}
		return withRateLimits(protocolErrorWithDetails(
			"STRATZ rejected the upstream request",
			details,
			nil,
		), rates)
	}

	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return withRateLimits(
			protocolErrorWithDetails("STRATZ returned an unexpected redirect", details, nil),
			rates,
		)
	}
	if response.StatusCode == http.StatusInternalServerError && looksLikeMalformedToken(body) {
		return withRateLimits(authenticationError(details), rates)
	}
	if response.StatusCode >= 500 {
		return &Error{
			Code:       contracts.ErrorCodeUpstreamError,
			Message:    "STRATZ returned a temporary server error",
			Details:    details,
			RateLimits: rates,
			Retryable:  true,
			RetryAfter: retryAfter,
		}
	}
	return withRateLimits(
		protocolErrorWithDetails("STRATZ returned an unexpected HTTP status", details, nil),
		rates,
	)
}

func classifyWAF(response *http.Response, body []byte) *Error {
	lowerBody := strings.ToLower(string(bodyPrefix(body, 4096)))
	contentType := strings.ToLower(headerValue(response.Header, "Content-Type"))
	server := strings.ToLower(headerValue(response.Header, "Server"))
	challenge := strings.EqualFold(headerValue(response.Header, "cf-mitigated"), "challenge") ||
		(strings.Contains(server, "cloudflare") &&
			(response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusServiceUnavailable) &&
			(strings.Contains(contentType, "html") || hasChallengeMarker(lowerBody))) ||
		(strings.Contains(contentType, "html") && hasChallengeMarker(lowerBody))
	if !challenge {
		return nil
	}
	return &Error{
		Code:      contracts.ErrorCodeUpstreamWAFBlocked,
		Message:   "A Cloudflare challenge blocked STRATZ; run doctor and check API access",
		Details:   map[string]any{},
		Retryable: false,
	}
}

func hasChallengeMarker(body string) bool {
	return strings.Contains(body, "just a moment") ||
		strings.Contains(body, "cf-chl-") ||
		strings.Contains(body, "cloudflare ray id") ||
		strings.Contains(body, "challenge-platform")
}

func authenticationError(details map[string]any) *Error {
	return &Error{
		Code:      contracts.ErrorCodeAuthenticationFailed,
		Message:   "STRATZ rejected the configured credential",
		Details:   details,
		Retryable: false,
	}
}

func responseDetails(response *http.Response, requestID string) map[string]any {
	details := map[string]any{
		"http_status": response.StatusCode,
	}
	if requestID != "" {
		details["request_id"] = requestID
	}
	if cfRay := safeHeaderValue(headerValue(response.Header, "Cf-Ray")); cfRay != "" {
		details["cf_ray"] = cfRay
	}
	return details
}

func retryAt(headers http.Header, rates []RateLimit, now time.Time) *time.Time {
	if parsed := parseRetryAfter(headerValue(headers, "Retry-After"), now); parsed != nil {
		return parsed
	}
	for _, rate := range rates {
		if rate.ResetAt != nil && (rate.Window == "unknown" || rate.Remaining != nil && *rate.Remaining == 0) {
			value := *rate.ResetAt
			return &value
		}
	}
	return nil
}

func parseRetryAfter(value string, now time.Time) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64((7*24*time.Hour)/time.Second) {
			return nil
		}
		result := now.Add(time.Duration(seconds) * time.Second).UTC()
		return &result
	}
	if parsed, err := http.ParseTime(value); err == nil {
		if parsed.Before(now) || parsed.After(now.Add(7*24*time.Hour)) {
			return nil
		}
		result := parsed.UTC()
		return &result
	}
	return nil
}

func looksLikeMalformedToken(body []byte) bool {
	var value struct {
		Message string `json:"message"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(value.Message), "An unexpected error occurred")
}

func isTLSError(err error) bool {
	var certificateVerification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	return errors.As(err, &certificateVerification) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostname) ||
		errors.As(err, &invalid) ||
		errors.As(err, &recordHeader)
}

func isTemporaryNetworkError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTemporary || dnsErr.IsTimeout
	}
	var netErr net.Error
	return errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary())
}

func safeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return ""
		}
	}
	return value
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := headerValue(headers, name); value != "" {
			return value
		}
	}
	return ""
}

func headerValue(headers http.Header, name string) string {
	for actual, values := range headers {
		if strings.EqualFold(actual, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func bodyPrefix(body []byte, limit int) []byte {
	if len(body) <= limit {
		return body
	}
	return body[:limit]
}
