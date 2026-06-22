package playermatch

import (
	"encoding/json"
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
	RadiantKills    *int64                `json:"radiantKills"`
	DireKills       *int64                `json:"direKills"`
	GameModeID      *int64                `json:"gameModeId"`
	LobbyTypeID     *int64                `json:"lobbyTypeId"`
	RegionID        *int64                `json:"regionId"`
	LeagueID        *int64                `json:"leagueId"`
	GameVersionID   *string               `json:"gameVersionId"`
	ParsedDateTime  *int64                `json:"parsedDateTime"`
	StatsDateTime   *int64                `json:"statsDateTime"`
	ParseStatus     string                `json:"parseStatus"`
	Players         []upstreamMatchPlayer `json:"players"`
	Objectives      []upstreamEvent       `json:"objectives"`
	Timeline        []upstreamEvent       `json:"timeline"`
	Fights          []upstreamFight       `json:"fights"`
	Economy         []upstreamEconomy     `json:"economy"`
}

type upstreamMatchPlayer struct {
	SteamAccountID *int64 `json:"steamAccountId"`
	HeroID         int64  `json:"heroId"`
	IsRadiant      bool   `json:"isRadiant"`
	PlayerSlot     int64  `json:"playerSlot"`
	Kills          int64  `json:"kills"`
	Deaths         int64  `json:"deaths"`
	Assists        int64  `json:"assists"`
	Networth       *int64 `json:"networth"`
	Level          *int64 `json:"level"`
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
