package batch

import (
	"errors"
	"reflect"
	"testing"
)

func TestNewPlanDeduplicatesAndPreservesOrder(t *testing.T) {
	plan, err := NewPlan([]string{"a", "b", "a", "c", "b"}, 25, func(input string) (string, error) {
		return input, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Unique(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("unique = %#v", got)
	}
	if got := plan.UniqueKeys(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("unique keys = %#v", got)
	}
}

func TestNewPlanValidation(t *testing.T) {
	_, err := NewPlan([]string{"a"}, 0, func(input string) (string, error) { return input, nil })
	if err == nil {
		t.Fatal("NewPlan accepted a zero maximum")
	}
	_, err = NewPlan([]string{}, 25, func(input string) (string, error) { return input, nil })
	if err == nil {
		t.Fatal("NewPlan accepted an empty batch")
	}
	_, err = NewPlan([]string{"a", "b"}, 1, func(input string) (string, error) { return input, nil })
	if err == nil {
		t.Fatal("NewPlan accepted an oversized batch")
	}
	_, err = NewPlan[string, string]([]string{"a"}, 25, nil)
	if err == nil {
		t.Fatal("NewPlan accepted a nil key function")
	}
	_, err = NewPlan([]string{"a"}, 25, func(string) (string, error) {
		return "", errors.New("invalid input")
	})
	if err == nil {
		t.Fatal("NewPlan accepted a key error")
	}
}

func TestReconstructRestoresDuplicateInputs(t *testing.T) {
	plan, err := NewPlan([]string{"a", "b", "a", "c"}, 25, func(input string) (string, error) {
		return input, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Reconstruct(plan, map[string]int{
		"a": 10,
		"b": 20,
		"c": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{10, 20, 10, 30}; !reflect.DeepEqual(output, want) {
		t.Fatalf("output = %#v, want %#v", output, want)
	}
}

func TestReconstructRejectsMissingResult(t *testing.T) {
	plan, err := NewPlan([]string{"a", "b"}, 25, func(input string) (string, error) {
		return input, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reconstruct(plan, map[string]int{"a": 1}); err == nil {
		t.Fatal("Reconstruct accepted missing results")
	}
}
