package stratz

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

var rateWindows = []struct {
	window string
	suffix string
}{
	{window: "second", suffix: "Second"},
	{window: "minute", suffix: "Minute"},
	{window: "hour", suffix: "Hour"},
	{window: "day", suffix: "Day"},
}

func parseRateLimits(headers http.Header, now time.Time) []RateLimit {
	result := make([]RateLimit, 0, 5)
	for _, definition := range rateWindows {
		limitHeader := "X-RateLimit-Limit-" + definition.suffix
		remainingHeader := "X-RateLimit-Remaining-" + definition.suffix
		limit := parseNonNegativeInt(headerValue(headers, limitHeader))
		remaining := parseNonNegativeInt(headerValue(headers, remainingHeader))
		if limit == nil && remaining == nil {
			continue
		}
		result = append(result, RateLimit{
			Window:    definition.window,
			Limit:     limit,
			Remaining: remaining,
			Source:    limitHeader + "," + remainingHeader,
		})
	}

	aggregateLimit := parseNonNegativeInt(headerValue(headers, "RateLimit-Limit"))
	aggregateRemaining := parseNonNegativeInt(headerValue(headers, "RateLimit-Remaining"))
	aggregateReset := parseResetSeconds(headerValue(headers, "RateLimit-Reset"), now)
	if aggregateLimit != nil || aggregateRemaining != nil || aggregateReset != nil {
		result = append(result, RateLimit{
			Window:    "unknown",
			Limit:     aggregateLimit,
			Remaining: aggregateRemaining,
			ResetAt:   aggregateReset,
			Source:    "RateLimit-Limit,RateLimit-Remaining,RateLimit-Reset",
		})
	}
	return result
}

func parseNonNegativeInt(value string) *int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

func parseResetSeconds(value string, now time.Time) *time.Time {
	seconds := parseNonNegativeInt(value)
	if seconds == nil || *seconds > int64((7*24*time.Hour)/time.Second) {
		return nil
	}
	result := now.Add(time.Duration(*seconds) * time.Second).UTC()
	return &result
}
