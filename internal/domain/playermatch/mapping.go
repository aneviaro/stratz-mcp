package playermatch

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func mapPlayer(source *upstreamPlayer) contracts.Player {
	accountID := source.SteamAccountID
	if accountID < 0 {
		accountID = 0
	}
	account := strconv.FormatInt(accountID, 10)
	steam := strconv.FormatUint(steamID64Base+uint64(accountID), 10)
	var displayName, avatar *string
	if source.Identity != nil {
		displayName = cleanString(source.Identity.Name, 256)
	}
	if displayName == nil && source.SteamAccount != nil {
		displayName = cleanString(source.SteamAccount.Name, 256)
	}
	if source.SteamAccount != nil {
		avatar = cleanURL(source.SteamAccount.Avatar)
	}
	player := contracts.Player{
		AccountID:   account,
		SteamID64:   &steam,
		DisplayName: displayName,
		AvatarURL:   avatar,
		IsPrivate:   source.IsPrivate,
		MatchCount:  nonNegative(source.MatchCount),
		WinCount:    nonNegative(source.WinCount),
		LastMatchAt: unixDate(source.LastMatchDate),
	}
	if len(source.Ranks) > 0 {
		player.Rank = &struct {
			LeaderboardRank *int64 `json:"leaderboard_rank"`
			RankTier        *int64 `json:"rank_tier"`
		}{
			RankTier: nonNegative(source.Ranks[0].Rank),
		}
	}
	return player
}

func mapSummary(source *upstreamMatch) contracts.MatchSummary {
	var leagueID *string
	if source.LeagueID != nil {
		value := strconv.FormatInt(*source.LeagueID, 10)
		leagueID = &value
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
		PatchID:         cleanString(source.GameVersionID.Value, 64),
		ParseStatus:     parseStatus(source),
	}
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

func mapMatch(source *upstreamMatch, detail contracts.DetailLevel) contracts.Match {
	summary := mapSummary(source)
	match := contracts.Match{
		MatchID:         summary.MatchID,
		StartedAt:       summary.StartedAt,
		DurationSeconds: summary.DurationSeconds,
		RadiantWin:      summary.RadiantWin,
		RadiantScore:    summary.RadiantScore,
		DireScore:       summary.DireScore,
		GameModeID:      summary.GameModeID,
		LobbyTypeID:     summary.LobbyTypeID,
		RegionID:        summary.RegionID,
		LeagueID:        summary.LeagueID,
		PatchID:         summary.PatchID,
		ParseStatus:     summary.ParseStatus,
		Players:         make([]contracts.MatchPlayer, 0, len(source.Players)),
	}
	for _, player := range source.Players {
		var accountID *string
		if player.SteamAccountID != nil && *player.SteamAccountID >= 0 {
			value := strconv.FormatInt(*player.SteamAccountID, 10)
			accountID = &value
		}
		team := "dire"
		if player.IsRadiant {
			team = "radiant"
		}
		won := false
		if source.DidRadiantWin != nil {
			won = *source.DidRadiantWin == player.IsRadiant
		}
		match.Players = append(match.Players, contracts.MatchPlayer{
			AccountID: accountID,
			HeroID:    player.HeroID,
			Team:      team,
			Position:  normalizedPlayerSlot(player.PlayerSlot),
			Kills:     maxZero(player.Kills),
			Deaths:    maxZero(player.Deaths),
			Assists:   maxZero(player.Assists),
			Networth:  nonNegative(player.Networth),
			Level:     nonNegative(player.Level),
			Won:       won,
		})
	}
	if detail == contracts.DetailLevelStandard || detail == contracts.DetailLevelFull {
		match.Objectives = mapObjectives(source.PlaybackData)
	}
	if detail == contracts.DetailLevelFull {
		match.Timeline = mapTimeline(source.PlaybackData)
		match.Fights = mapFights(source.Fights)
		match.Economy = mapEconomy(source.Economy)
	}
	return match
}

func mapObjectives(source *upstreamPlaybackData) []contracts.TimelineEvent {
	if source == nil {
		return []contracts.TimelineEvent{}
	}
	events := make([]upstreamEvent, 0,
		len(source.BuildingEvents)+len(source.RoshanEvents)+len(source.TowerDeathEvents))
	for _, event := range source.BuildingEvents {
		events = append(events, upstreamEvent{
			Time: event.Time, Type: "building_" + cleanValue(event.Type, 48),
			IsRadiant: event.IsRadiant, Value: event.NPCID,
		})
	}
	for _, event := range source.RoshanEvents {
		events = append(events, upstreamEvent{Time: event.Time, Type: "roshan"})
	}
	for _, event := range source.TowerDeathEvents {
		events = append(events, upstreamEvent{
			Time: event.Time, Type: "tower_death",
			Value: map[string]int64{"radiant": event.Radiant, "dire": event.Dire},
		})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time < events[j].Time })
	return mapEvents(events)
}

func mapTimeline(source *upstreamPlaybackData) []contracts.TimelineEvent {
	if source == nil {
		return []contracts.TimelineEvent{}
	}
	events := make([]upstreamEvent, 0, len(source.RuneEvents)+len(source.WardEvents))
	for _, event := range source.RuneEvents {
		events = append(events, upstreamEvent{
			Time: event.Time, Type: "rune",
			Value: map[string]int64{"action": event.Action, "rune": event.Rune},
		})
	}
	for _, event := range source.WardEvents {
		value := cleanValue(event.WardType, 64)
		if event.FromPlayer != nil {
			value = fmt.Sprintf("ward_type=%s;from_player=%d", value, *event.FromPlayer)
		}
		events = append(events, upstreamEvent{
			Time: event.Time, Type: "ward_" + cleanValue(event.Action, 32),
			Value: value,
		})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time < events[j].Time })
	return mapEvents(events)
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

func mapEvents(source []upstreamEvent) []contracts.TimelineEvent {
	result := make([]contracts.TimelineEvent, 0, len(source))
	if len(source) > 5000 {
		source = source[:5000]
	}
	for _, event := range source {
		var team *string
		if event.IsRadiant != nil {
			value := "dire"
			if *event.IsRadiant {
				value = "radiant"
			}
			team = &value
		}
		var account *string
		if event.SteamAccountID != nil && *event.SteamAccountID >= 0 {
			value := strconv.FormatInt(*event.SteamAccountID, 10)
			account = &value
		}
		result = append(result, contracts.TimelineEvent{
			TimeSeconds: maxZero(event.Time),
			Type:        cleanValue(event.Type, 64),
			Team:        team,
			AccountID:   account,
			HeroID:      event.HeroID,
			Value:       scalarValue(event.Value),
		})
	}
	return result
}

func mapFights(source []upstreamFight) []contracts.Fight {
	if len(source) > 500 {
		source = source[:500]
	}
	result := make([]contracts.Fight, 0, len(source))
	for _, fight := range source {
		item := contracts.Fight{
			StartTimeSeconds:     maxZero(fight.StartTime),
			EndTimeSeconds:       maxZero(fight.EndTime),
			RadiantKills:         maxZero(fight.RadiantKills),
			DireKills:            maxZero(fight.DireKills),
			RadiantNetworthDelta: fight.RadiantNetworthDelta,
		}
		if len(fight.Participants) > 10 {
			fight.Participants = fight.Participants[:10]
		}
		for _, participant := range fight.Participants {
			var account *string
			if participant.SteamAccountID != nil && *participant.SteamAccountID >= 0 {
				value := strconv.FormatInt(*participant.SteamAccountID, 10)
				account = &value
			}
			team := "dire"
			if participant.IsRadiant {
				team = "radiant"
			}
			item.Participants = append(item.Participants, struct {
				AccountID *string `json:"account_id"`
				Deaths    int64   `json:"deaths"`
				HeroID    int64   `json:"hero_id"`
				Kills     int64   `json:"kills"`
				Team      string  `json:"team"`
			}{
				AccountID: account,
				HeroID:    participant.HeroID,
				Team:      team,
				Kills:     maxZero(participant.Kills),
				Deaths:    maxZero(participant.Deaths),
			})
		}
		result = append(result, item)
	}
	return result
}

func mapEconomy(source []upstreamEconomy) []contracts.EconomyPoint {
	if len(source) > 5000 {
		source = source[:5000]
	}
	result := make([]contracts.EconomyPoint, 0, len(source))
	for _, point := range source {
		result = append(result, contracts.EconomyPoint{
			TimeSeconds:       maxZero(point.Time),
			RadiantNetworth:   nonNegative(point.RadiantNetworth),
			DireNetworth:      nonNegative(point.DireNetworth),
			RadiantExperience: nonNegative(point.RadiantExperience),
			DireExperience:    nonNegative(point.DireExperience),
		})
	}
	return result
}

func parseStatus(match *upstreamMatch) string {
	switch strings.ToLower(strings.TrimSpace(match.ParseStatus)) {
	case "parsed", "partial", "pending", "unavailable", "unknown":
		return strings.ToLower(strings.TrimSpace(match.ParseStatus))
	}
	if match.ParsedDateTime != nil {
		return "parsed"
	}
	if match.StatsDateTime != nil {
		return "partial"
	}
	if match.DurationSeconds == nil {
		return "pending"
	}
	return "unavailable"
}

func unixDate(value *int64) *contracts.DateTime {
	if value == nil || *value < 0 {
		return nil
	}
	formatted := contracts.DateTime(time.Unix(*value, 0).UTC().Format(time.RFC3339))
	return &formatted
}

func nonNegative(value *int64) *int64 {
	if value == nil || *value < 0 {
		return nil
	}
	copy := *value
	return &copy
}

func positive(value *int64) *int64 {
	if value == nil || *value < 1 {
		return nil
	}
	copy := *value
	return &copy
}

func maxZero(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func cleanString(value *string, limit int) *string {
	if value == nil {
		return nil
	}
	cleaned := cleanValue(*value, limit)
	return &cleaned
}

func cleanURL(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if !strings.HasPrefix(cleaned, "https://") && !strings.HasPrefix(cleaned, "http://") {
		return nil
	}
	return cleanString(&cleaned, 2048)
}

func cleanValue(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	for utf8.RuneCountInString(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func scalarValue(value any) any {
	switch typed := value.(type) {
	case nil, bool, string, float64, int, int64, jsonNumber:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

type jsonNumber interface {
	String() string
}
