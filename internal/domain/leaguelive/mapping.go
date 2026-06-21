package leaguelive

import (
	"strconv"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func mapLeague(source *upstreamLeague, now time.Time) contracts.League {
	name := clean(source.Name, 256)
	if name == "" && source.DisplayName != nil {
		name = clean(*source.DisplayName, 256)
	}
	status := deriveStatus(source, now)
	return contracts.League{
		LeagueID: strconv.FormatInt(source.ID, 10),
		Name:     name,
		Tier:     cleanPointer(source.Tier, 64),
		Status:   &status,
		Region:   cleanPointer(source.Region, 128),
		StartsAt: unixDate(source.StartDateTime),
		EndsAt:   unixDate(source.EndDateTime),
	}
}

func deriveStatus(source *upstreamLeague, now time.Time) string {
	if source.IsLive != nil && *source.IsLive {
		return "live"
	}
	if source.IsEnded != nil && *source.IsEnded {
		return "completed"
	}
	if source.IsFuture != nil && *source.IsFuture {
		return "upcoming"
	}
	now = now.UTC()
	if source.EndDateTime != nil && !time.Unix(*source.EndDateTime, 0).After(now) {
		return "completed"
	}
	if source.StartDateTime != nil && time.Unix(*source.StartDateTime, 0).After(now) {
		return "upcoming"
	}
	if source.StartDateTime != nil {
		return "ongoing"
	}
	return "unknown"
}

func mapSummary(source *upstreamMatch) contracts.MatchSummary {
	var leagueID *string
	if source.LeagueID != nil {
		value := strconv.FormatInt(*source.LeagueID, 10)
		leagueID = &value
	}
	status := "pending"
	if source.ParsedDateTime != nil {
		status = "parsed"
	} else if source.StatsDateTime != nil {
		status = "partial"
	} else if source.DurationSeconds != nil {
		status = "unavailable"
	}
	return contracts.MatchSummary{
		MatchID:         contracts.MatchID(strconv.FormatInt(source.ID, 10)),
		StartedAt:       unixDate(source.StartDateTime),
		DurationSeconds: nonNegative(source.DurationSeconds),
		RadiantWin:      source.DidRadiantWin,
		RadiantScore:    nonNegative(source.RadiantKills),
		DireScore:       nonNegative(source.DireKills),
		GameModeID:      source.GameModeID,
		LobbyTypeID:     source.LobbyTypeID,
		RegionID:        source.RegionID,
		LeagueID:        leagueID,
		PatchID:         cleanPointer(source.GameVersionID, 64),
		ParseStatus:     status,
	}
}

func mapLive(source *upstreamLiveMatch, now time.Time) contracts.LiveMatch {
	result := contracts.LiveMatch{
		MatchID:         contracts.MatchID(strconv.FormatInt(source.ID, 10)),
		StartedAt:       unixDate(source.StartDateTime),
		DurationSeconds: nonNegative(source.GameTime),
		GameModeID:      source.GameModeID,
		SpectatorCount:  nonNegative(source.SpectatorCount),
		RadiantTeamName: cleanPointer(source.RadiantTeamName, 256),
		DireTeamName:    cleanPointer(source.DireTeamName, 256),
		Players:         make([]contracts.LiveMatchPlayer, 0, len(source.Players)),
	}
	if source.League != nil {
		league := mapLeague(source.League, now)
		result.League = &league
	}
	for _, player := range source.Players {
		var account *string
		if player.SteamAccountID != nil && *player.SteamAccountID >= 0 {
			value := strconv.FormatInt(*player.SteamAccountID, 10)
			account = &value
		}
		team := "dire"
		if player.IsRadiant {
			team = "radiant"
		}
		var heroID *int64
		if player.HeroID > 0 {
			value := player.HeroID
			heroID = &value
		}
		result.Players = append(result.Players, contracts.LiveMatchPlayer{
			AccountID: account,
			HeroID:    heroID,
			Team:      team,
			Position:  maxZero(player.PlayerSlot),
			Kills:     maxZero(player.Kills),
			Deaths:    maxZero(player.Deaths),
			Assists:   maxZero(player.Assists),
			Networth:  nonNegative(player.Networth),
			Level:     nonNegative(player.Level),
			Won:       nil,
		})
	}
	return result
}

func unixDate(value *int64) *contracts.DateTime {
	if value == nil || *value < 0 {
		return nil
	}
	date := contracts.DateTime(time.Unix(*value, 0).UTC().Format(time.RFC3339))
	return &date
}

func cleanPointer(value *string, limit int) *string {
	if value == nil {
		return nil
	}
	cleaned := clean(*value, limit)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func clean(value string, limit int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value))
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func nonNegative(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := maxZero(*value)
	return &copy
}

func maxZero(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
