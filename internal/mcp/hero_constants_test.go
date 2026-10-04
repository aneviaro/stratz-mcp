package mcp

import (
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/heroconstants"
)

func TestHeroConstantsQueryEnvelopesValidate(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	options := Options{
		Version: "test", SchemaVersion: "sha256:fixture", Config: config.Defaults(t.TempDir()),
		Now: func() time.Time { return now },
	}
	heroResult := &heroconstants.Result[heroconstants.HeroQueryData]{
		Data: heroconstants.HeroQueryData{
			Mode: "exact",
			Items: []contracts.Hero{{
				HeroID: 1, Name: "npc_dota_hero_axe", Slug: "axe", Roles: []string{},
			}},
		},
	}
	if _, err := SuccessResult(
		"stratz_query_heroes",
		heroConstantsEnvelope(options, "query_heroes", "", heroResult, false),
	); err != nil {
		t.Fatal(err)
	}

	constantsResult := &heroconstants.Result[heroconstants.ConstantsQueryData]{
		Data: heroconstants.ConstantsQueryData{
			Mode: "types",
			Items: []contracts.Constant{{
				ID: "1", Name: "axe", Type: "heroes", Metadata: map[string]any{},
			}},
		},
	}
	if _, err := SuccessResult(
		"stratz_query_constants",
		heroConstantsEnvelope(options, "query_constants", "", constantsResult, false),
	); err != nil {
		t.Fatal(err)
	}
}
