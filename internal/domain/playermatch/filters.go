package playermatch

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

const playerHistoryRoleLaneUnavailableMessage = "Player-history role and lane filtering is temporarily unavailable because current STRATZ rejects roleIds and laneIds request fields"

// ParsePlayerHistoryFilters validates and normalizes the primitive filters for
// player history. Numeric hero IDs are parsed here; display-selector hero
// resolution remains adapter-specific.
func ParsePlayerHistoryFilters(input map[string]any) (PlayerMatchFilters, *Error) {
	for key := range input {
		switch key {
		case "mode", "cursor", "detail_level", "fresh", "include_raw", "player_id", "from", "to", "hero", "role", "position", "lane", "game_mode_id", "lobby_type_id", "result", "minimum_duration_seconds", "patch_id", "limit":
		default:
			return PlayerMatchFilters{}, invalid("Unsupported player-history field", map[string]any{"field": key})
		}
	}
	player, ok := input["player_id"].(string)
	if !ok || strings.TrimSpace(player) == "" {
		return PlayerMatchFilters{}, invalid("player_id is required", nil)
	}
	filters := PlayerMatchFilters{PlayerID: player}
	if value, exists := input["limit"]; exists {
		number, parseErr := ParseHistoryInteger(value, "limit")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		if number > int64(maxInt()) || number < int64(minInt()) {
			return PlayerMatchFilters{}, invalid("limit must be an integer", nil)
		}
		filters.Limit = int(number)
	}
	if value, exists := input["cursor"]; exists {
		cursor, cursorOK := value.(string)
		if !cursorOK {
			return PlayerMatchFilters{}, invalid("cursor must be a string", nil)
		}
		filters.Cursor = cursor
	}
	if value, exists := input["from"]; exists {
		parsed, parseErr := ParseHistoryDate(value, "from")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.From = parsed
	}
	if value, exists := input["to"]; exists {
		parsed, parseErr := ParseHistoryDate(value, "to")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.To = parsed
	}
	if value, exists := input["hero"]; exists {
		id, parseErr := ParseHistoryInteger(value, "hero")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.HeroID = &id
	}
	if value, exists := input["role"]; exists {
		role, roleID, parseErr := ParseHistoryRole(value)
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.Role = &role
		filters.roleID = &roleID
	}
	if value, exists := input["position"]; exists {
		position, parseErr := ParseHistoryPosition(value)
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.Position = &position
	}
	if value, exists := input["lane"]; exists {
		lane, parseErr := ParseHistoryLane(value)
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.Lane = &lane
	}
	for key, target := range map[string]**int64{
		"game_mode_id":             &filters.GameModeID,
		"lobby_type_id":            &filters.LobbyTypeID,
		"minimum_duration_seconds": &filters.MinimumDurationSeconds,
	} {
		if value, exists := input[key]; exists {
			parsed, parseErr := ParseHistoryInteger(value, key)
			if parseErr != nil {
				return PlayerMatchFilters{}, parseErr
			}
			*target = &parsed
		}
	}
	if value, exists := input["result"]; exists {
		result, resultOK := value.(string)
		if !resultOK || (result != "win" && result != "loss") {
			return PlayerMatchFilters{}, invalid("result must be win or loss", nil)
		}
		filters.Result = &result
	}
	if value, exists := input["patch_id"]; exists {
		patch, patchID, parseErr := ParseHistoryPatch(value)
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.PatchID = &patch
		filters.patchID = &patchID
	}
	if err := normalizePlayerHistoryFilters(&filters); err != nil {
		return PlayerMatchFilters{}, err
	}
	return filters, nil
}

// ParseHistoryDate is the shared RFC3339 date-time parser for history filters.
func ParseHistoryDate(value any, field string) (*time.Time, *Error) {
	text, ok := value.(string)
	if !ok {
		return nil, invalid(field+" must be an RFC3339 date-time", nil)
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil, invalid(field+" must be an RFC3339 date-time", nil)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

// ParseHistoryInteger parses the JSON-compatible integer forms accepted by
// history filters. It intentionally does not apply field-specific ranges.
func ParseHistoryInteger(value any, field string) (int64, *Error) {
	var parsed int64
	switch number := value.(type) {
	case int:
		parsed = int64(number)
	case int64:
		parsed = number
	case int32:
		parsed = int64(number)
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt64 || number > math.MaxInt64 {
			return 0, invalid(field+" must be an integer", nil)
		}
		parsed = int64(number)
	case json.Number:
		value, err := number.Int64()
		if err != nil {
			return 0, invalid(field+" must be an integer", nil)
		}
		parsed = value
	case string:
		value, err := strconv.ParseInt(strings.TrimSpace(number), 10, 64)
		if err != nil {
			return 0, invalid(field+" must be an integer", nil)
		}
		parsed = value
	default:
		return 0, invalid(field+" must be an integer", nil)
	}
	return parsed, nil
}

// ParseHistoryPatch validates the public decimal patch ID and returns its
// canonical string and GraphQL Int value.
func ParseHistoryPatch(value any) (string, int64, *Error) {
	text, ok := value.(string)
	if !ok || text == "" || text[0] < '1' || text[0] > '9' {
		return "", 0, invalid("patch_id must be a positive base-10 integer", nil)
	}
	for index := 1; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return "", 0, invalid("patch_id must be a positive base-10 integer", nil)
		}
	}
	parsed, err := strconv.ParseInt(text, 10, 32)
	if err != nil || parsed < 1 {
		return "", 0, invalid("patch_id must be a positive base-10 integer", nil)
	}
	return text, parsed, nil
}

// ParseHistoryRole maps the public role enum to STRATZ role IDs.
func ParseHistoryRole(value any) (string, int64, *Error) {
	role, ok := value.(string)
	if !ok {
		return "", 0, invalid("role must be CORE, LIGHT_SUPPORT, or HARD_SUPPORT", nil)
	}
	var id int64
	switch role {
	case "CORE":
		id = 0
	case "LIGHT_SUPPORT":
		id = 1
	case "HARD_SUPPORT":
		id = 2
	default:
		return "", 0, invalid("role must be CORE, LIGHT_SUPPORT, or HARD_SUPPORT", nil)
	}
	return role, id, nil
}

// ParseHistoryPosition validates the public position enum.
func ParseHistoryPosition(value any) (string, *Error) {
	position, ok := value.(string)
	if !ok {
		return "", invalid("position must be POSITION_1 through POSITION_5", nil)
	}
	switch position {
	case "POSITION_1", "POSITION_2", "POSITION_3", "POSITION_4", "POSITION_5":
		return position, nil
	default:
		return "", invalid("position must be POSITION_1 through POSITION_5", nil)
	}
}

// ParseHistoryLane maps the public lane enum to STRATZ lane IDs.
func ParseHistoryLane(value any) (int64, *Error) {
	lane, ok := value.(string)
	if !ok {
		return 0, invalid("lane must be ROAMING, SAFE_LANE, MID_LANE, OFF_LANE, or JUNGLE", nil)
	}
	switch lane {
	case "ROAMING":
		return 0, nil
	case "SAFE_LANE":
		return 1, nil
	case "MID_LANE":
		return 2, nil
	case "OFF_LANE":
		return 3, nil
	case "JUNGLE":
		return 4, nil
	default:
		return 0, invalid("lane must be ROAMING, SAFE_LANE, MID_LANE, OFF_LANE, or JUNGLE", nil)
	}
}

func normalizePlayerHistoryFilters(filters *PlayerMatchFilters) *Error {
	if filters.From != nil && filters.To != nil && filters.From.After(*filters.To) {
		return invalid("Player match from date must not be after to date", nil)
	}
	if filters.Role != nil {
		if filters.roleID == nil {
			role, id, err := ParseHistoryRole(*filters.Role)
			if err != nil {
				return err
			}
			filters.Role = &role
			filters.roleID = &id
		}
		return unavailablePlayerHistoryRoleLaneFilter("role")
	}
	if filters.Position != nil {
		position, err := ParseHistoryPosition(*filters.Position)
		if err != nil {
			return err
		}
		filters.Position = &position
	}
	if filters.Lane != nil {
		if *filters.Lane < 0 || *filters.Lane > 4 {
			return invalid("lane must be between 0 and 4", nil)
		}
		return unavailablePlayerHistoryRoleLaneFilter("lane")
	}
	if filters.Result != nil && *filters.Result != "win" && *filters.Result != "loss" {
		return invalid("result must be win or loss", nil)
	}
	if filters.PatchID != nil && filters.patchID == nil {
		patch, patchID, err := ParseHistoryPatch(*filters.PatchID)
		if err != nil {
			return err
		}
		filters.PatchID = &patch
		filters.patchID = &patchID
	}
	return nil
}

func unavailablePlayerHistoryRoleLaneFilter(field string) *Error {
	return invalid(playerHistoryRoleLaneUnavailableMessage, map[string]any{
		"field":  field,
		"reason": "current STRATZ rejects roleIds and laneIds in PlayerMatchesRequestType",
	})
}

func maxInt() int { return int(^uint(0) >> 1) }
func minInt() int { return -maxInt() - 1 }
