package playermatch

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

// Error is a stable player/match domain failure.
type Error struct {
	Code        contracts.ErrorCode
	Message     string
	Retryable   bool
	RetryAfter  *time.Time
	Details     map[string]any
	FailedInput any
	Context     any
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// Result carries normalized data and safe response metadata.
type Result[T any] struct {
	Data       T
	Raw        any
	RateLimits []stratz.RateLimit
	Warnings   []string
}

// PlayerQueryData is the normalized result of the exact player query.
type PlayerQueryData struct {
	Mode  string
	Items []contracts.Player
}

// MatchQueryData is the tagged match-query result. Items is intentionally an
// interface because each mode owns a distinct item shape.
type MatchQueryData struct {
	Mode  string          `json:"mode"`
	Items any             `json:"items"`
	Page  *contracts.Page `json:"page,omitempty"`
}

// The following legacy match shapes remain domain-local while the v2 contract
// exposes the consolidated query types.
type PageInfo struct {
	NextCursor *string
	HasMore    bool
}

type PlayerMatchesData struct {
	Items []PlayerMatchSummary
	Page  PageInfo
}

type PlayerMatchSummary struct {
	MatchID         contracts.MatchID          `json:"match_id"`
	StartedAt       contracts.NullableDateTime `json:"started_at"`
	DurationSeconds *int64                     `json:"duration_seconds"`
	RadiantWin      *bool                      `json:"radiant_win"`
	RadiantScore    *int64                     `json:"radiant_score"`
	DireScore       *int64                     `json:"dire_score"`
	GameModeID      *int64                     `json:"game_mode_id"`
	LobbyTypeID     *int64                     `json:"lobby_type_id"`
	RegionID        *int64                     `json:"region_id"`
	LeagueID        *string                    `json:"league_id"`
	PatchID         *string                    `json:"patch_id"`
	ParseStatus     string                     `json:"parse_status"`
	Player          *MatchPlayer               `json:"player,omitempty"`
}

type MatchPlayer struct {
	AccountID *string  `json:"account_id"`
	HeroID    int64    `json:"hero_id"`
	HeroName  *string  `json:"-"`
	Team      string   `json:"team"`
	Position  int64    `json:"position"`
	Kills     int64    `json:"kills"`
	Deaths    int64    `json:"deaths"`
	Assists   int64    `json:"assists"`
	Networth  *int64   `json:"networth"`
	Level     *int64   `json:"level"`
	Imp       *float64 `json:"-"`
	Won       *bool    `json:"won"`
}

type Match struct {
	MatchID         contracts.MatchID          `json:"match_id"`
	StartedAt       contracts.NullableDateTime `json:"started_at"`
	DurationSeconds *int64                     `json:"duration_seconds"`
	RadiantWin      *bool                      `json:"radiant_win"`
	RadiantScore    *int64                     `json:"radiant_score"`
	DireScore       *int64                     `json:"dire_score"`
	GameModeID      *int64                     `json:"game_mode_id"`
	LobbyTypeID     *int64                     `json:"lobby_type_id"`
	RegionID        *int64                     `json:"region_id"`
	LeagueID        *string                    `json:"league_id"`
	PatchID         *string                    `json:"patch_id"`
	ParseStatus     string                     `json:"parse_status"`
	Players         []MatchPlayer              `json:"players"`
	Objectives      []TimelineEvent            `json:"objectives,omitempty"`
	Timeline        []TimelineEvent            `json:"timeline,omitempty"`
	Fights          []Fight                    `json:"-"`
	Economy         []EconomyPoint             `json:"-"`
}

type TimelineEvent struct {
	TimeSeconds int64   `json:"time_seconds"`
	Type        string  `json:"type"`
	Team        *string `json:"team"`
	AccountID   *string `json:"account_id"`
	HeroID      *int64  `json:"hero_id"`
	HeroName    *string `json:"-"`
	Value       any     `json:"value"`
}

type Fight struct {
	StartTimeSeconds     int64
	EndTimeSeconds       int64
	RadiantKills         int64
	DireKills            int64
	RadiantNetworthDelta *int64
	Participants         []struct {
		AccountID *string `json:"account_id"`
		Deaths    int64   `json:"deaths"`
		HeroID    int64   `json:"hero_id"`
		HeroName  *string `json:"hero_name"`
		Kills     int64   `json:"kills"`
		Team      string  `json:"team"`
	}
}

type EconomyPoint struct {
	TimeSeconds       int64
	RadiantNetworth   *int64
	DireNetworth      *int64
	RadiantExperience *int64
	DireExperience    *int64
}

type upstreamPlayer struct {
	SteamAccountID int64 `json:"steamAccountId"`
	SteamAccount   *struct {
		ID     *int64  `json:"id"`
		Name   *string `json:"name"`
		Avatar *string `json:"avatar"`
	} `json:"steamAccount"`
	Identity *struct {
		Name *string `json:"name"`
	} `json:"identity"`
	MatchCount    *int64 `json:"matchCount"`
	WinCount      *int64 `json:"winCount"`
	LastMatchDate *int64 `json:"lastMatchDate"`
	IsPrivate     bool   `json:"isPrivate"`
	Ranks         []struct {
		Rank *int64 `json:"rank"`
	} `json:"ranks"`
}

type upstreamMatch struct {
	ID              int64                 `json:"id"`
	StartDateTime   *int64                `json:"startDateTime"`
	DurationSeconds *int64                `json:"durationSeconds"`
	DidRadiantWin   *bool                 `json:"didRadiantWin"`
	RadiantKills    upstreamKillCount     `json:"radiantKills"`
	DireKills       upstreamKillCount     `json:"direKills"`
	GameModeID      upstreamEnumID        `json:"gameModeId"`
	LobbyTypeID     upstreamEnumID        `json:"lobbyTypeId"`
	RegionID        *int64                `json:"regionId"`
	LeagueID        *int64                `json:"leagueId"`
	GameVersionID   upstreamString        `json:"gameVersionId"`
	ParsedDateTime  *int64                `json:"parsedDateTime"`
	StatsDateTime   *int64                `json:"statsDateTime"`
	ParseStatus     string                `json:"parseStatus"`
	Players         []upstreamMatchPlayer `json:"players"`
	PlaybackData    *upstreamPlaybackData `json:"playbackData"`
	Fights          []upstreamFight       `json:"fights"`
	Economy         []upstreamEconomy     `json:"economy"`
}

type upstreamPlaybackData struct {
	BuildingEvents []struct {
		Time      int64  `json:"time"`
		Type      string `json:"type"`
		IsRadiant *bool  `json:"isRadiant"`
		NPCID     *int64 `json:"npcId"`
	} `json:"buildingEvents"`
	RoshanEvents []struct {
		Time int64 `json:"time"`
	} `json:"roshanEvents"`
	TowerDeathEvents []struct {
		Time    int64 `json:"time"`
		Radiant int64 `json:"radiant"`
		Dire    int64 `json:"dire"`
	} `json:"towerDeathEvents"`
	RuneEvents []struct {
		Time   int64 `json:"time"`
		Action int64 `json:"action"`
		Rune   int64 `json:"rune"`
	} `json:"runeEvents"`
	WardEvents []struct {
		Time       int64  `json:"time"`
		Action     string `json:"action"`
		WardType   string `json:"wardType"`
		FromPlayer *int64 `json:"fromPlayer"`
	} `json:"wardEvents"`
}

type upstreamKillCount struct {
	Value *int64
}

func (value *upstreamKillCount) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		value.Value = nil
		return nil
	}
	var direct int64
	if err := json.Unmarshal(data, &direct); err == nil {
		value.Value = &direct
		return nil
	}
	var events []int64
	if err := json.Unmarshal(data, &events); err != nil {
		return err
	}
	count := int64(len(events))
	value.Value = &count
	return nil
}

type upstreamString struct {
	Value *string
}

func (value *upstreamString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		value.Value = nil
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		value.Value = &text
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	text = number.String()
	if _, err := strconv.ParseInt(text, 10, 64); err != nil {
		return err
	}
	value.Value = &text
	return nil
}

type upstreamEnumID struct {
	Number *int64
	Name   string
}

func (value *upstreamEnumID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		value.Number = nil
		value.Name = ""
		return nil
	}
	var number int64
	if err := json.Unmarshal(data, &number); err == nil {
		value.Number = &number
		value.Name = ""
		return nil
	}
	if err := json.Unmarshal(data, &value.Name); err != nil {
		return err
	}
	value.Number = nil
	return nil
}

type upstreamMatchPlayer struct {
	SteamAccountID *int64   `json:"steamAccountId"`
	HeroID         int64    `json:"heroId"`
	IsRadiant      bool     `json:"isRadiant"`
	PlayerSlot     int64    `json:"playerSlot"`
	Kills          int64    `json:"kills"`
	Deaths         int64    `json:"deaths"`
	Assists        int64    `json:"assists"`
	Networth       *int64   `json:"networth"`
	Level          *int64   `json:"level"`
	IMP            *float64 `json:"imp"`
}

type upstreamEvent struct {
	Time           int64  `json:"time"`
	Type           string `json:"type"`
	IsRadiant      *bool  `json:"isRadiant"`
	SteamAccountID *int64 `json:"steamAccountId"`
	HeroID         *int64 `json:"heroId"`
	Value          any    `json:"value"`
}

type upstreamFight struct {
	StartTime            int64  `json:"startTime"`
	EndTime              int64  `json:"endTime"`
	RadiantKills         int64  `json:"radiantKills"`
	DireKills            int64  `json:"direKills"`
	RadiantNetworthDelta *int64 `json:"radiantNetworthDelta"`
	Participants         []struct {
		SteamAccountID *int64 `json:"steamAccountId"`
		HeroID         int64  `json:"heroId"`
		IsRadiant      bool   `json:"isRadiant"`
		Kills          int64  `json:"kills"`
		Deaths         int64  `json:"deaths"`
	} `json:"participants"`
}

type upstreamEconomy struct {
	Time              int64  `json:"time"`
	RadiantNetworth   *int64 `json:"radiantNetworth"`
	DireNetworth      *int64 `json:"direNetworth"`
	RadiantExperience *int64 `json:"radiantExperience"`
	DireExperience    *int64 `json:"direExperience"`
}

type playerEnvelope struct {
	Player *upstreamPlayer `json:"player"`
}

type playersEnvelope struct {
	Players []*upstreamPlayer `json:"players"`
}

type matchEnvelope struct {
	Match *upstreamMatch `json:"match"`
}

type matchesEnvelope struct {
	Matches []*upstreamMatch `json:"matches"`
}

type playerMatchesEnvelope struct {
	Player *struct {
		SteamAccountID int64           `json:"steamAccountId"`
		Matches        []upstreamMatch `json:"matches"`
	} `json:"player"`
}

func decodeData[T any](data json.RawMessage, output *T) error {
	return json.Unmarshal(data, output)
}
