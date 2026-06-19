package mcp

import (
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/leaguelive"
)

func TestLeagueLiveEnvelopesValidate(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	options := Options{
		Version: "test", SchemaVersion: "sha256:fixture", Config: config.Defaults(t.TempDir()),
		Now: func() time.Time { return now },
	}
	league := contracts.League{LeagueID: "1", Name: "League", Status: stringPointer("live")}
	if _, err := SuccessResult("stratz_get_league", leagueLiveEnvelope(
		options, "get_league", contracts.DetailLevelStandard,
		&leaguelive.Result[contracts.League]{Data: league}, false, nil,
	)); err != nil {
		t.Fatal(err)
	}
	live := contracts.StratzListLiveMatchesData{
		Items: []contracts.LiveMatch{}, Page: contracts.PageInfo{HasMore: true, NextCursor: stringPointer("cursor")},
	}
	if _, err := SuccessResult("stratz_list_live_matches", leagueLiveEnvelope(
		options, "list_live_matches", "",
		&leaguelive.Result[contracts.StratzListLiveMatchesData]{
			Data: live, Warnings: []string{"not a snapshot"},
		}, false, nil,
	)); err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string { return &value }
