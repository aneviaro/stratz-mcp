package mcp

import (
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/leaguelive"
)

func TestQueryLeagueAndMatchEnvelopesValidate(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	options := Options{
		Version: "test", SchemaVersion: "sha256:fixture", Config: config.Defaults(t.TempDir()),
		Now: func() time.Time { return now },
	}
	leagueResult := &leaguelive.Result[leaguelive.LeagueQueryData]{
		Data: leaguelive.LeagueQueryData{
			Mode:  "exact",
			Items: []contracts.League{{LeagueID: "1", Name: "League"}},
		},
	}
	if _, err := SuccessResult("stratz_query_leagues", leagueLiveEnvelope(
		options, "query_leagues", "", leagueResult, false, nil,
		map[string]any{"mode": "exact", "items": leagueResult.Data.Items},
	)); err != nil {
		t.Fatal(err)
	}

	matchResult := &leaguelive.Result[leaguelive.MatchQueryData]{
		Data: leaguelive.MatchQueryData{
			Mode: "live", Items: []contracts.LiveMatch{{MatchID: "1", Players: []contracts.LivePlayer{}}},
			Page: &contracts.Page{HasMore: false},
		},
	}
	if _, err := SuccessResult("stratz_query_matches", queryMatchesEnvelope(
		options, matchResult.Data, nil, nil, nil, false, "",
	)); err != nil {
		t.Fatal(err)
	}
}
