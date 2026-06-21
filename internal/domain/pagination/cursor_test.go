package pagination

import (
	"reflect"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func TestFilterHashCanonicalOrdering(t *testing.T) {
	left, err := FilterHash(map[string]any{
		"hero_id":   1,
		"player_id": "123",
		"range": map[string]any{
			"to":   "2026-06-19",
			"from": "2026-06-01",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	right, err := FilterHash(map[string]any{
		"range": map[string]any{
			"from": "2026-06-01",
			"to":   "2026-06-19",
		},
		"player_id": "123",
		"hero_id":   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("filter hash mismatch: %q != %q", left, right)
	}
}

func TestCodecRoundTrip(t *testing.T) {
	now := time.Date(2026, time.June, 19, 12, 0, 0, 0, time.UTC)
	codec := NewCodec(Options{Now: func() time.Time { return now }})
	next := "after:20"
	state := ScanState[string, int]{
		Next:            &next,
		HasMoreUpstream: true,
	}
	binding := testBinding()

	cursor, err := codec.Encode(binding, LifetimeRecent, state)
	if err != nil {
		t.Fatal(err)
	}
	var decodedState ScanState[string, int]
	payload, err := codec.Decode(cursor, binding, &decodedState)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Tool != binding.Tool || payload.PageSize != binding.PageSize {
		t.Fatalf("payload binding = %#v", payload)
	}
	if want := now.Add(time.Hour); !payload.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %s, want %s", payload.ExpiresAt, want)
	}
	if !reflect.DeepEqual(decodedState, state) {
		t.Fatalf("decoded state = %#v, want %#v", decodedState, state)
	}
}

func TestCodecRejectsMismatchedBindingsAndExpiry(t *testing.T) {
	now := time.Date(2026, time.June, 19, 12, 0, 0, 0, time.UTC)
	binding := testBinding()
	cursor, err := NewCodec(Options{Now: func() time.Time { return now }}).Encode(
		binding,
		LifetimeRecent,
		map[string]any{"after": "cursor"},
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		cursor  string
		binding Binding
		now     time.Time
		want    contracts.ErrorCode
	}{
		{
			name:    "tampered",
			cursor:  cursor + "x",
			binding: binding,
			now:     now,
			want:    contracts.ErrorCodeCursorInvalid,
		},
		{
			name:   "wrong tool",
			cursor: cursor,
			binding: func() Binding {
				copy := binding
				copy.Tool = "stratz_list_live_matches"
				return copy
			}(),
			now:  now,
			want: contracts.ErrorCodeCursorInvalid,
		},
		{
			name:   "wrong filters",
			cursor: cursor,
			binding: func() Binding {
				copy := binding
				copy.Filters = map[string]any{"player_id": "321"}
				return copy
			}(),
			now:  now,
			want: contracts.ErrorCodeCursorInvalid,
		},
		{
			name:   "wrong token",
			cursor: cursor,
			binding: func() Binding {
				copy := binding
				copy.Token = "rotated-token"
				return copy
			}(),
			now:  now,
			want: contracts.ErrorCodeCursorInvalid,
		},
		{
			name:   "wrong schema version",
			cursor: cursor,
			binding: func() Binding {
				copy := binding
				copy.SchemaVersion = "sha256:other"
				return copy
			}(),
			now:  now,
			want: contracts.ErrorCodeCursorInvalid,
		},
		{
			name:   "wrong operation version",
			cursor: cursor,
			binding: func() Binding {
				copy := binding
				copy.OperationVersion = "matches/v2"
				return copy
			}(),
			now:  now,
			want: contracts.ErrorCodeCursorInvalid,
		},
		{
			name:   "wrong page size",
			cursor: cursor,
			binding: func() Binding {
				copy := binding
				copy.PageSize = 10
				return copy
			}(),
			now:  now,
			want: contracts.ErrorCodeCursorInvalid,
		},
		{
			name:    "expired",
			cursor:  cursor,
			binding: binding,
			now:     now.Add(time.Hour + time.Second),
			want:    contracts.ErrorCodeCursorExpired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCodec(Options{Now: func() time.Time { return test.now }}).Decode(
				test.cursor,
				test.binding,
				nil,
			)
			assertCursorCode(t, err, test.want)
		})
	}
}

func TestCodecIsRestartStable(t *testing.T) {
	now := time.Date(2026, time.June, 19, 12, 0, 0, 0, time.UTC)
	binding := testBinding()
	first, err := NewCodec(Options{Now: func() time.Time { return now }}).Encode(
		binding,
		LifetimeHistorical,
		map[string]any{"page": 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCodec(Options{Now: func() time.Time { return now }}).Encode(
		binding,
		LifetimeHistorical,
		map[string]any{"page": 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("cursor mismatch across restarts:\n%s\n%s", first, second)
	}
}

func testBinding() Binding {
	return Binding{
		Tool:             "stratz_list_player_matches",
		Filters:          map[string]any{"player_id": "123", "hero_id": 1},
		PageSize:         25,
		Token:            "fixture-token",
		SchemaVersion:    "sha256:fixture",
		OperationVersion: "matches/v1",
	}
}

func assertCursorCode(t *testing.T, err error, want contracts.ErrorCode) {
	t.Helper()
	cursorErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error = %T %v, want *Error", err, err)
	}
	if cursorErr.Code != want {
		t.Fatalf("code = %s, want %s", cursorErr.Code, want)
	}
}
