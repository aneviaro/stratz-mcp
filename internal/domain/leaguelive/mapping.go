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
		RadiantScore:    nonNegative(source.RadiantKills.Value),
		DireScore:       nonNegative(source.DireKills.Value),
		GameModeID:      enumID(source.GameModeID, gameModeIDs),
		LobbyTypeID:     enumID(source.LobbyTypeID, lobbyTypeIDs),
		RegionID:        source.RegionID,
		LeagueID:        leagueID,
		PatchID:         cleanPointer(source.GameVersionID.Value, 64),
		ParseStatus:     status,
	}
}

func mapLive(source *upstreamLiveMatch, now time.Time) contracts.LiveMatch {
	result := contracts.LiveMatch{
		MatchID:         contracts.MatchID(strconv.FormatInt(source.ID, 10)),
		StartedAt:       unixDate(source.StartDateTime),
		DurationSeconds: nonNegative(source.GameTime),
		GameModeID:      enumID(source.GameModeID, gameModeIDs),
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
			Position:  normalizedPlayerSlot(player.PlayerSlot),
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

var gameModeIDs = map[string]int64{
	"NONE": 0, "ALL_PICK": 1, "CAPTAINS_MODE": 2, "RANDOM_DRAFT": 3,
	"SINGLE_DRAFT": 4, "ALL_RANDOM": 5, "INTRO": 6, "DIRETIDE": 7,
	"REVERSE_CAPTAINS_MODE": 8, "GREEVILING": 9, "TUTORIAL": 10,
	"MID_ONLY": 11, "LEAST_PLAYED": 12, "LIMITED_HEROES": 13,
	"COMPENDIUM_MATCHMAKING": 14, "CUSTOM": 15, "CAPTAINS_DRAFT": 16,
	"BALANCED_DRAFT": 17, "ABILITY_DRAFT": 18, "EVENT": 19,
	"ALL_RANDOM_DEATH_MATCH": 20, "ONE_VS_ONE_MID": 21, "ALL_DRAFT": 22,
	"ALL_PICK_RANKED": 22, "TURBO": 23, "MUTATION": 24,
}

var lobbyTypeIDs = map[string]int64{
	"UNRANKED": 0, "PRACTICE": 1, "TOURNAMENT": 2, "TUTORIAL": 3,
	"COOP_BOTS": 4, "RANKED_TEAM": 5, "RANKED_SOLO": 6, "RANKED": 7,
	"ONE_VS_ONE_MID": 8, "BATTLE_CUP": 9, "LOCAL_BOTS": 10,
	"SPECTATOR": 11, "EVENT": 12, "GAUNTLET": 13, "NEW_PLAYER": 14,
	"FEATURED": 15,
}

func enumID(value upstreamEnumID, known map[string]int64) *int64 {
	if value.Number != nil {
		return nonNegative(value.Number)
	}
	number, ok := known[value.Name]
	if !ok {
		return nil
	}
	return &number
}

func normalizedPlayerSlot(slot int64) int64 {
	if slot >= 128 && slot <= 132 {
		return slot - 123
	}
	if slot < 0 {
		return 0
	}
	if slot > 9 {
		return 9
	}
	return slot
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
