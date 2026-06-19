// Package playermatch implements the curated player and match domains.
package playermatch

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

const steamID64Base uint64 = 76561197960265728

// PlayerID is a canonical Steam account identifier pair.
type PlayerID struct {
	AccountID uint32
	SteamID64 uint64
}

// NormalizePlayerID accepts a 32-bit account ID, SteamID64, or STRATZ profile URL.
func NormalizePlayerID(value string) (PlayerID, error) {
	original := strings.TrimSpace(value)
	if original == "" {
		return PlayerID{}, invalid("Player identifier is required", nil)
	}
	candidate := original
	if strings.Contains(candidate, "://") {
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Scheme != "https" {
			return PlayerID{}, invalid("STRATZ profile URL must use https", nil)
		}
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if host != "stratz.com" && host != "www.stratz.com" {
			return PlayerID{}, invalid("Player URL must be a stratz.com profile URL", nil)
		}
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) != 2 || parts[0] != "players" {
			return PlayerID{}, invalid("STRATZ profile URL must have the form /players/{id}", nil)
		}
		decoded, err := url.PathUnescape(parts[1])
		if err != nil {
			return PlayerID{}, invalid("STRATZ profile URL contains an invalid player ID", nil)
		}
		candidate = decoded
	}
	if candidate == "" || strings.IndexFunc(candidate, func(r rune) bool {
		return r < '0' || r > '9'
	}) >= 0 {
		return PlayerID{}, invalid("Player identifier must contain only decimal digits", nil)
	}
	numeric, err := strconv.ParseUint(candidate, 10, 64)
	if err != nil {
		return PlayerID{}, invalid("Player identifier is outside the supported range", nil)
	}
	switch {
	case numeric <= math.MaxUint32:
		return PlayerID{AccountID: uint32(numeric), SteamID64: steamID64Base + numeric}, nil
	case numeric >= steamID64Base && numeric <= steamID64Base+math.MaxUint32:
		return PlayerID{AccountID: uint32(numeric - steamID64Base), SteamID64: numeric}, nil
	default:
		return PlayerID{}, invalid("Player identifier is neither an account ID nor a valid SteamID64", nil)
	}
}

// NormalizeMatchID validates a GraphQL Long-compatible decimal match ID.
func NormalizeMatchID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.IndexFunc(value, func(r rune) bool {
		return r < '0' || r > '9'
	}) >= 0 {
		return 0, invalid("Match ID must contain only decimal digits", nil)
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return 0, invalid("Match ID is outside the supported GraphQL Long range", nil)
	}
	return id, nil
}

func invalid(message string, details map[string]any) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{
		Code:    contracts.ErrorCodeInvalidArgument,
		Message: message,
		Details: details,
	}
}

func playerKey(id PlayerID) string {
	return strconv.FormatUint(uint64(id.AccountID), 10)
}

func matchKey(id int64) string {
	return strconv.FormatInt(id, 10)
}

func failedInput(index int, value any) map[string]any {
	return map[string]any{"index": index, "value": value}
}

func requireDetail(value contracts.DetailLevel) (contracts.DetailLevel, error) {
	if value == "" {
		return contracts.DetailLevelStandard, nil
	}
	switch value {
	case contracts.DetailLevelSummary, contracts.DetailLevelStandard, contracts.DetailLevelFull:
		return value, nil
	default:
		return "", invalid(fmt.Sprintf("Unsupported detail level %q", value), nil)
	}
}
