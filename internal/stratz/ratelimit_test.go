package stratz

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRateLimitsModelsIndependentAndAggregateWindows(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	headers := http.Header{
		"X-RateLimit-Limit-Second":     []string{"8"},
		"X-RateLimit-Remaining-Second": []string{"7"},
		"X-RateLimit-Limit-Minute":     []string{"150"},
		"X-RateLimit-Remaining-Minute": []string{"149"},
		"X-RateLimit-Limit-Hour":       []string{"1500"},
		"X-RateLimit-Remaining-Hour":   []string{"1499"},
		"X-RateLimit-Limit-Day":        []string{"15000"},
		"X-RateLimit-Remaining-Day":    []string{"14999"},
		"RateLimit-Limit":              []string{"8"},
		"RateLimit-Remaining":          []string{"7"},
		"RateLimit-Reset":              []string{"1"},
		"X-SteamId":                    []string{"must-not-appear"},
		"Arbitrary-Header":             []string{"must-not-appear"},
	}
	rates := parseRateLimits(headers, now)
	if len(rates) != 5 {
		t.Fatalf("rate windows = %d, want 5: %#v", len(rates), rates)
	}
	expectedWindows := []string{"second", "minute", "hour", "day", "unknown"}
	for index, expected := range expectedWindows {
		if rates[index].Window != expected {
			t.Errorf("window[%d] = %q, want %q", index, rates[index].Window, expected)
		}
		if rates[index].Source == "" ||
			rates[index].Source == "X-SteamId" ||
			rates[index].Source == "Arbitrary-Header" {
			t.Errorf("unsafe or absent source: %#v", rates[index])
		}
	}
	if rates[0].Limit == nil || *rates[0].Limit != 8 ||
		rates[0].Remaining == nil || *rates[0].Remaining != 7 {
		t.Fatalf("second window = %#v", rates[0])
	}
	if rates[4].ResetAt == nil || !rates[4].ResetAt.Equal(now.Add(time.Second)) {
		t.Fatalf("aggregate reset = %#v", rates[4].ResetAt)
	}
}

func TestParseRateLimitsIgnoresMalformedAndUnsafeValues(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	headers := http.Header{
		"X-RateLimit-Limit-Second":     []string{"-1"},
		"X-RateLimit-Remaining-Second": []string{"secret"},
		"RateLimit-Limit":              []string{"9223372036854775808"},
		"RateLimit-Reset":              []string{"999999999"},
	}
	if rates := parseRateLimits(headers, now); len(rates) != 0 {
		t.Fatalf("malformed rates were accepted: %#v", rates)
	}
}

func TestParseRetryAfterSupportsSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	if parsed := parseRetryAfter("3", now); parsed == nil || !parsed.Equal(now.Add(3*time.Second)) {
		t.Fatalf("numeric Retry-After = %#v", parsed)
	}
	date := now.Add(time.Minute).Format(http.TimeFormat)
	if parsed := parseRetryAfter(date, now); parsed == nil || !parsed.Equal(now.Add(time.Minute)) {
		t.Fatalf("date Retry-After = %#v", parsed)
	}
	for _, invalid := range []string{"", "-1", "999999999", "not-a-date"} {
		if parsed := parseRetryAfter(invalid, now); parsed != nil {
			t.Fatalf("Retry-After %q = %v, want nil", invalid, parsed)
		}
	}
}
