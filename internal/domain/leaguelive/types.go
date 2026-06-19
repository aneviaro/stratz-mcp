package leaguelive

import (
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

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

type Result[T any] struct {
	Data       T
	Raw        any
	RateLimits []stratz.RateLimit
	Warnings   []string
}

type LeagueFilters struct {
	Query  *string
	Status *string
	Tier   *string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor string
}

type LeagueMatchFilters struct {
	LeagueID string
	From     *time.Time
	To       *time.Time
	PatchID  *string
	Limit    int
	Cursor   string
}

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
	ID              int64   `json:"id"`
	StartDateTime   *int64  `json:"startDateTime"`
	DurationSeconds *int64  `json:"durationSeconds"`
	DidRadiantWin   *bool   `json:"didRadiantWin"`
	RadiantKills    *int64  `json:"radiantKills"`
	DireKills       *int64  `json:"direKills"`
	GameModeID      *int64  `json:"gameModeId"`
	LobbyTypeID     *int64  `json:"lobbyTypeId"`
	RegionID        *int64  `json:"regionId"`
	LeagueID        *int64  `json:"leagueId"`
	GameVersionID   *string `json:"gameVersionId"`
	ParsedDateTime  *int64  `json:"parsedDateTime"`
	StatsDateTime   *int64  `json:"statsDateTime"`
}

type upstreamLiveMatch struct {
	ID              int64                `json:"id"`
	StartDateTime   *int64               `json:"startDateTime"`
	GameTime        *int64               `json:"gameTime"`
	GameModeID      *int64               `json:"gameModeId"`
	SpectatorCount  *int64               `json:"spectatorCount"`
	RadiantTeamID   *int64               `json:"radiantTeamId"`
	DireTeamID      *int64               `json:"direTeamId"`
	RadiantTeamName *string              `json:"radiantTeamName"`
	DireTeamName    *string              `json:"direTeamName"`
	League          *upstreamLeague      `json:"league"`
	Players         []upstreamLivePlayer `json:"players"`
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
