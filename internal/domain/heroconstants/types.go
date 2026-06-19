// Package heroconstants implements curated hero reference, constants, and
// aggregate hero-statistics operations.
package heroconstants

import (
	"encoding/json"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

type Error struct {
	Code        contracts.ErrorCode
	Message     string
	Retryable   bool
	RetryAfter  *time.Time
	Details     map[string]any
	FailedInput any
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

type Result[T any] struct {
	Data           T
	Raw            any
	RateLimits     []stratz.RateLimit
	Warnings       []string
	EffectiveRange *DateRange
	PatchID        *string
}

type DateRange struct {
	From time.Time
	To   time.Time
}

type StatsFilters struct {
	Hero             any
	From             *time.Time
	To               *time.Time
	PatchID          *string
	RankBracket      *string
	Role             *string
	Lane             *string
	IncludeMatchups  bool
	IncludeSynergies bool
}

type upstreamHero struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	LocalizedName    *string  `json:"localizedName"`
	PrimaryAttribute *string  `json:"primaryAttribute"`
	AttackType       *string  `json:"attackType"`
	Roles            []string `json:"roles"`
}

type upstreamMetadata struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

type upstreamConstant struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	LocalizedName *string            `json:"localizedName"`
	Metadata      []upstreamMetadata `json:"metadata"`
}

type upstreamConstants struct {
	Heroes    []upstreamHero     `json:"heroes"`
	Items     []upstreamConstant `json:"items"`
	Abilities []upstreamConstant `json:"abilities"`
	GameModes []upstreamConstant `json:"gameModes"`
	Regions   []upstreamConstant `json:"regions"`
	Ranks     []upstreamConstant `json:"ranks"`
}

type constantsEnvelope struct {
	Constants upstreamConstants `json:"constants"`
}

type upstreamBreakdown struct {
	Name                 string `json:"name"`
	MatchCount           int64  `json:"matchCount"`
	PickCount            int64  `json:"pickCount"`
	WinCount             int64  `json:"winCount"`
	PopulationMatchCount int64  `json:"populationMatchCount"`
}

type upstreamRelation struct {
	HeroID          int64    `json:"heroId"`
	MatchCount      int64    `json:"matchCount"`
	WinCount        int64    `json:"winCount"`
	ExpectedWinRate *float64 `json:"expectedWinRate"`
}

type upstreamStats struct {
	HeroID               int64               `json:"heroId"`
	MatchCount           int64               `json:"matchCount"`
	PickCount            int64               `json:"pickCount"`
	WinCount             int64               `json:"winCount"`
	BanCount             int64               `json:"banCount"`
	PopulationMatchCount int64               `json:"populationMatchCount"`
	RankDataAvailable    *bool               `json:"rankDataAvailable"`
	Roles                []upstreamBreakdown `json:"roles"`
	Lanes                []upstreamBreakdown `json:"lanes"`
	Matchups             []upstreamRelation  `json:"matchups"`
	Synergies            []upstreamRelation  `json:"synergies"`
}

type statsEnvelope struct {
	HeroStats *upstreamStats `json:"heroStats"`
}

func rawData(data json.RawMessage) any {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
}
