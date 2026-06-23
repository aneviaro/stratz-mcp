// Package heroconstants implements curated hero reference, constants, and
// aggregate hero-statistics operations.
package heroconstants

import (
	"bytes"
	"encoding/json"
	"strconv"
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
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	LocalizedName *string `json:"localizedName"`
	Stats         *struct {
		PrimaryAttribute *string `json:"primaryAttribute"`
		AttackType       *string `json:"attackType"`
	} `json:"stats"`
	Roles []struct {
		RoleID *string `json:"roleId"`
	} `json:"roles"`
}

func (hero *upstreamHero) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID               int64   `json:"id"`
		Name             string  `json:"name"`
		LocalizedName    *string `json:"localizedName"`
		PrimaryAttribute *string `json:"primaryAttribute"`
		AttackType       *string `json:"attackType"`
		Stats            *struct {
			PrimaryAttribute *string `json:"primaryAttribute"`
			AttackType       *string `json:"attackType"`
		} `json:"stats"`
		Roles []json.RawMessage `json:"roles"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	hero.ID = raw.ID
	hero.Name = raw.Name
	hero.LocalizedName = raw.LocalizedName
	hero.Stats = raw.Stats
	if hero.Stats == nil && (raw.PrimaryAttribute != nil || raw.AttackType != nil) {
		hero.Stats = &struct {
			PrimaryAttribute *string `json:"primaryAttribute"`
			AttackType       *string `json:"attackType"`
		}{
			PrimaryAttribute: raw.PrimaryAttribute,
			AttackType:       raw.AttackType,
		}
	}
	hero.Roles = make([]struct {
		RoleID *string `json:"roleId"`
	}, 0, len(raw.Roles))
	for _, roleData := range raw.Roles {
		var name string
		if err := json.Unmarshal(roleData, &name); err != nil {
			var object struct {
				RoleID *string `json:"roleId"`
			}
			if err := json.Unmarshal(roleData, &object); err != nil {
				return err
			}
			hero.Roles = append(hero.Roles, object)
			continue
		}
		hero.Roles = append(hero.Roles, struct {
			RoleID *string `json:"roleId"`
		}{RoleID: &name})
	}
	return nil
}

type upstreamMetadata struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

type upstreamConstant struct {
	ID            upstreamConstantID `json:"id"`
	Name          string             `json:"name"`
	LocalizedName *string            `json:"localizedName"`
	Language      *struct {
		DisplayName *string `json:"displayName"`
	} `json:"language"`
}

type upstreamConstantID string

func (value *upstreamConstantID) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = upstreamConstantID(text)
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
	*value = upstreamConstantID(text)
	return nil
}

func (value upstreamConstantID) String() string {
	return string(bytes.TrimSpace([]byte(value)))
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
	Period               int64               `json:"period"`
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
	HeroStats *struct {
		Stats []upstreamStats `json:"stats"`
	} `json:"heroStats"`
}

func rawData(data json.RawMessage) any {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
}
