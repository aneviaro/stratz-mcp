package mcp

import (
	"errors"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

func TestInputObject(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantErr bool
	}{
		{name: "object", input: map[string]any{"key": "value"}},
		{name: "nil", input: nil, wantErr: true},
		{name: "scalar", input: "not an object", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := inputObject(test.input)
			if (err != nil) != test.wantErr {
				t.Errorf("inputObject(%#v) error presence = %t, want %t", test.input, err != nil, test.wantErr)
			}
			if !test.wantErr && got == nil {
				t.Errorf("inputObject(%#v) = nil, want an argument object", test.input)
			}
		})
	}
}

func TestRequiredString(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		key     string
		want    string
		wantErr bool
	}{
		{name: "present", input: map[string]any{"id": "42"}, key: "id", want: "42"},
		{name: "missing", input: map[string]any{}, key: "id", wantErr: true},
		{name: "wrong type", input: map[string]any{"id": 42}, key: "id", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := requiredString(test.input, test.key)
			if (err != nil) != test.wantErr {
				t.Errorf("requiredString(%#v, %q) error presence = %t, want %t", test.input, test.key, err != nil, test.wantErr)
			}
			if got != test.want {
				t.Errorf("requiredString(%#v, %q) = %q, want %q", test.input, test.key, got, test.want)
			}
			if test.wantErr {
				var executionErr *ExecutionError
				if !errors.As(err, &executionErr) || executionErr.Code != contracts.ErrorCodeInvalidArgument {
					t.Errorf("requiredString(%#v, %q) error = %v, want INVALID_ARGUMENT", test.input, test.key, err)
				}
			}
		})
	}
}
