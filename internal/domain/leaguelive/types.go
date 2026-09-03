package leaguelive

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

// Error is a stable league-live domain failure.
type Error struct {
	Code       contracts.ErrorCode
	Message    string
	Retryable  bool
	RetryAfter *time.Time
	Details    map[string]any
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// Result carries normalized league-live data and safe response metadata.
type Result[T any] struct {
	Data       T
	Raw        any
	RateLimits []stratz.RateLimit
	Warnings   []string
}

// LeagueFilters contains the bounded filters for league listing.
type LeagueFilters struct {
	Query  *string
	Status *string
	Tier   *string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor string
}

// LeagueMatchFilters contains the bounded filters for league-match listing.
type LeagueMatchFilters struct {
	LeagueID string
	From     *time.Time
	To       *time.Time
	PatchID  *string
	Limit    int
	Cursor   string
}

// LiveFilters contains the bounded filters for live-match listing.
type LiveFilters struct {
	PlayerID          *int64
	TeamID            *int64
	LeagueID          *int64
	HeroID            *int64
	GameStates        []string
	Tiers             []string
	GameModeID        *int64
	MinimumSpectators *int64
	Sort              string
	Limit             int
	Cursor            string
}

type upstreamLeague struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	DisplayName   *string `json:"displayName"`
	Region        *string `json:"region"`
	Tier          *string `json:"tier"`
	StartDateTime *int64  `json:"startDateTime"`
	EndDateTime   *int64  `json:"endDateTime"`
	IsFuture      *bool   `json:"isFuture"`
	IsEnded       *bool   `json:"isEnded"`
	IsLive        *bool   `json:"isLive"`
}

type upstreamMatch struct {
	ID              int64             `json:"id"`
	StartDateTime   *int64            `json:"startDateTime"`
	DurationSeconds *int64            `json:"durationSeconds"`
	DidRadiantWin   *bool             `json:"didRadiantWin"`
	RadiantKills    upstreamKillCount `json:"radiantKills"`
	DireKills       upstreamKillCount `json:"direKills"`
	GameModeID      upstreamEnumID    `json:"gameModeId"`
	LobbyTypeID     upstreamEnumID    `json:"lobbyTypeId"`
	RegionID        *int64            `json:"regionId"`
	LeagueID        *int64            `json:"leagueId"`
	GameVersionID   upstreamString    `json:"gameVersionId"`
	ParsedDateTime  *int64            `json:"parsedDateTime"`
	StatsDateTime   *int64            `json:"statsDateTime"`
}

type upstreamLiveMatch struct {
	ID              int64                `json:"id"`
	StartDateTime   *int64               `json:"startDateTime"`
	GameTime        *int64               `json:"gameTime"`
	GameModeID      upstreamEnumID       `json:"gameModeId"`
	SpectatorCount  *int64               `json:"spectatorCount"`
	RadiantTeamID   *int64               `json:"radiantTeamId"`
	DireTeamID      *int64               `json:"direTeamId"`
	RadiantTeamName *string              `json:"radiantTeamName"`
	DireTeamName    *string              `json:"direTeamName"`
	League          *upstreamLeague      `json:"league"`
	Players         []upstreamLivePlayer `json:"players"`
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

type upstreamLivePlayer struct {
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

type leagueEnvelope struct {
	League *upstreamLeague `json:"league"`
}

type leaguesEnvelope struct {
	Leagues []upstreamLeague `json:"leagues"`
}

type leagueMatchesEnvelope struct {
	League *struct {
		ID      int64           `json:"id"`
		Matches []upstreamMatch `json:"matches"`
	} `json:"league"`
}

type liveEnvelope struct {
	Live struct {
		Matches []upstreamLiveMatch `json:"matches"`
	} `json:"live"`
}
