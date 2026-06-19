package mcp

import (
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
)

func TestHeroConstantsEnvelopesValidate(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	options := Options{
		Version: "test", SchemaVersion: "sha256:fixture", Config: config.Defaults(t.TempDir()),
		Now: func() time.Time { return now },
	}
	heroResult := &heroconstants.Result[contracts.Hero]{
		Data: contracts.Hero{
			HeroID: 1, Name: "npc_dota_hero_axe", Slug: "axe", Roles: []string{"Initiator"},
		},
	}
	if _, err := SuccessResult(
		"stratz_get_hero",
		heroConstantsEnvelope(options, "get_hero", contracts.DetailLevelStandard, heroResult, false),
	); err != nil {
		t.Fatal(err)
	}

	statsResult := &heroconstants.Result[contracts.StratzGetHeroStatsData]{
		Data: contracts.StratzGetHeroStatsData{
			HeroID: 1, Roles: []contracts.HeroBreakdown{}, Lanes: []contracts.HeroBreakdown{},
			Matchups: []contracts.HeroRelation{}, Synergies: []contracts.HeroRelation{},
		},
		Warnings: []string{"rank data unavailable"},
		EffectiveRange: &heroconstants.DateRange{
			From: now.AddDate(0, 0, -7), To: now,
		},
	}
	if _, err := SuccessResult(
		"stratz_get_hero_stats",
		heroConstantsEnvelope(options, "get_hero_stats", "", statsResult, false),
	); err != nil {
		t.Fatal(err)
	}

	constantsResult := &heroconstants.Result[contracts.StratzGetConstantsData]{
		Data:     contracts.StratzGetConstantsData{Type: "ranks", Items: []contracts.ConstantRecord{}},
		Warnings: []string{"rank constants unavailable"},
	}
	if _, err := SuccessResult(
		"stratz_get_constants",
		heroConstantsEnvelope(options, "get_constants", "", constantsResult, false),
	); err != nil {
		t.Fatal(err)
	}
}
