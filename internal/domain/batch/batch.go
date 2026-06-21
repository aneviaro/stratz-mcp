// Package batch implements reusable curated-tool batch planning helpers.
package batch

import (
	"errors"
	"fmt"
)

// Plan preserves the caller's original inputs while deduplicating a stable
// unique sequence for upstream work.
type Plan[Input any, Key comparable] struct {
	inputs     []Input
	inputKeys  []Key
	unique     []Input
	uniqueKeys []Key
}

// NewPlan validates one bounded batch input slice and records the exact
// duplicate-preserving reconstruction order.
func NewPlan[Input any, Key comparable](
	inputs []Input,
	maximum int,
	keyFunc func(Input) (Key, error),
) (*Plan[Input, Key], error) {
	switch {
	case maximum < 1:
		return nil, errors.New("batch maximum must be positive")
	case len(inputs) == 0:
		return nil, errors.New("batch input is required")
	case len(inputs) > maximum:
		return nil, fmt.Errorf("batch input exceeds the maximum size of %d", maximum)
	case keyFunc == nil:
		return nil, errors.New("batch key function is required")
	}

	plan := &Plan[Input, Key]{
		inputs:    append([]Input(nil), inputs...),
		inputKeys: make([]Key, len(inputs)),
	}
	seen := make(map[Key]int, len(inputs))
	for index, input := range inputs {
		key, err := keyFunc(input)
		if err != nil {
			return nil, fmt.Errorf("batch input %d: %w", index, err)
		}
		plan.inputKeys[index] = key
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = len(plan.unique)
		plan.unique = append(plan.unique, input)
		plan.uniqueKeys = append(plan.uniqueKeys, key)
	}
	return plan, nil
}

// Inputs returns the original duplicate-preserving batch input order.
func (plan *Plan[Input, Key]) Inputs() []Input {
	if plan == nil {
		return nil
	}
	return append([]Input(nil), plan.inputs...)
}

// Unique returns the stable deduplicated input order.
func (plan *Plan[Input, Key]) Unique() []Input {
	if plan == nil {
		return nil
	}
	return append([]Input(nil), plan.unique...)
}

// UniqueKeys returns the keys for the deduplicated input order.
func (plan *Plan[Input, Key]) UniqueKeys() []Key {
	if plan == nil {
		return nil
	}
	return append([]Key(nil), plan.uniqueKeys...)
}

// Reconstruct restores exact caller order and duplicates from unique results.
func Reconstruct[Input any, Key comparable, Result any](
	plan *Plan[Input, Key],
	results map[Key]Result,
) ([]Result, error) {
	switch {
	case plan == nil:
		return nil, errors.New("batch plan is required")
	case results == nil:
		return nil, errors.New("batch results are required")
	}

	output := make([]Result, len(plan.inputKeys))
	for index, key := range plan.inputKeys {
		result, ok := results[key]
		if !ok {
			return nil, fmt.Errorf("missing batch result for key %v", key)
		}
		output[index] = result
	}
	return output, nil
}
