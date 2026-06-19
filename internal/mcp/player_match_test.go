package mcp

import (
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func TestCuratedPlayerAndMatchEnvelopesValidate(t *testing.T) {
	options := Options{
		Version:       "test",
		SchemaVersion: "sha256:fixture",
		Config:        config.Defaults(t.TempDir()),
		Now: func() time.Time {
			return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
		},
	}
	player := contracts.Player{
		AccountID: "1",
		IsPrivate: false,
	}
	playerEnvelope := curatedEnvelope(
		options,
		"get_player",
		contracts.DetailLevelSummary,
		player,
		map[string]any{"player": map[string]any{"steamAccountId": 1}},
		true,
		nil,
		nil,
	)
	if _, err := SuccessResult("stratz_get_player", playerEnvelope); err != nil {
		t.Fatal(err)
	}

	match := contracts.Match{
		MatchID:     "1",
		ParseStatus: "parsed",
		Players:     []contracts.MatchPlayer{},
	}
	matchEnvelope := curatedEnvelope(
		options,
		"get_match",
		contracts.DetailLevelSummary,
		match,
		nil,
		false,
		nil,
		nil,
	)
	if _, err := SuccessResult("stratz_get_match", matchEnvelope); err != nil {
		t.Fatal(err)
	}
}

func TestDataNotReadyErrorEnvelopeValidates(t *testing.T) {
	err := &ExecutionError{
		Code:      contracts.ErrorCodeDataNotReady,
		Message:   "not ready",
		Details:   map[string]any{"parse_status": "pending"},
		Retryable: false,
		Context: contracts.MatchAvailabilityContext{
			Type: "match_availability",
			Match: contracts.MatchSummary{
				MatchID:     "1",
				ParseStatus: "pending",
			},
			RequestedDetailLevel: contracts.DetailLevelFull,
		},
	}
	result, encodeErr := ErrorResult("stratz_get_match", err)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if !result.IsError {
		t.Fatal("DATA_NOT_READY did not use the MCP error path")
	}
}
