package pagination

import (
	"context"
	"errors"
)

// Page is one fetched upstream page before client-side filtering.
type Page[Cursor any, Item any] struct {
	Items   []Item
	Next    *Cursor
	HasMore bool
}

// ScanState preserves only the upstream continuation point for a later call.
type ScanState[Cursor any] struct {
	Next            *Cursor `json:"next,omitempty"`
	HasMoreUpstream bool    `json:"has_more_upstream"`
}

// ScanOptions configures one bounded client-side scan pass.
type ScanOptions[Cursor any, Item any] struct {
	Limit    int
	MaxPages int
	State    *ScanState[Cursor]
	Fetch    func(context.Context, *Cursor) (Page[Cursor, Item], error)
	Accept   func(Item) bool
	Advance  func(*Cursor, int) *Cursor
}

// ScanResult contains the filtered items and any continuation state.
type ScanResult[Cursor any, Item any] struct {
	Items        []Item
	Next         *ScanState[Cursor]
	HasMore      bool
	PagesScanned int
}

// Scan continues a bounded client-side filtered list traversal.
func Scan[Cursor any, Item any](
	ctx context.Context,
	options ScanOptions[Cursor, Item],
) (*ScanResult[Cursor, Item], error) {
	switch {
	case options.Limit < 1:
		return nil, errors.New("scan limit must be positive")
	case options.MaxPages < 1:
		return nil, errors.New("scan max pages must be positive")
	case options.Fetch == nil:
		return nil, errors.New("scan fetch function is required")
	}
	if options.Accept == nil {
		options.Accept = func(Item) bool { return true }
	}

	result := &ScanResult[Cursor, Item]{
		Items: make([]Item, 0, options.Limit),
	}
	state := cloneState(options.State)
	if state != nil && !state.HasMoreUpstream && state.Next == nil {
		result.Next = normalizeState(state)
		result.HasMore = result.Next != nil
		return result, nil
	}

	var next *Cursor
	if state != nil {
		next = clonePointer(state.Next)
	}
	hasMoreUpstream := state != nil && state.HasMoreUpstream

	for result.PagesScanned < options.MaxPages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := clonePointer(next)
		page, err := options.Fetch(ctx, start)
		if err != nil {
			return nil, err
		}
		result.PagesScanned++
		next = clonePointer(page.Next)
		hasMoreUpstream = page.HasMore
		if !page.HasMore {
			next = nil
		}

		for index, item := range page.Items {
			if options.Accept(item) {
				result.Items = append(result.Items, item)
				if len(result.Items) == options.Limit {
					continuation := clonePointer(next)
					more := hasMoreUpstream
					if index+1 < len(page.Items) {
						if options.Advance == nil {
							return nil, errors.New("scan advance function is required for partial pages")
						}
						continuation = options.Advance(start, index+1)
						more = true
					}
					result.Next = normalizeState(&ScanState[Cursor]{
						Next:            continuation,
						HasMoreUpstream: more,
					})
					result.HasMore = result.Next != nil
					return result, nil
				}
			}
		}
		if !hasMoreUpstream {
			return result, nil
		}
	}

	result.Next = normalizeState(&ScanState[Cursor]{
		Next:            clonePointer(next),
		HasMoreUpstream: hasMoreUpstream,
	})
	result.HasMore = result.Next != nil
	return result, nil
}

func normalizeState[Cursor any](
	state *ScanState[Cursor],
) *ScanState[Cursor] {
	if state == nil {
		return nil
	}
	state = cloneState(state)
	if !state.HasMoreUpstream {
		state.Next = nil
	}
	if state.Next == nil && !state.HasMoreUpstream {
		return nil
	}
	return state
}

func cloneState[Cursor any](
	state *ScanState[Cursor],
) *ScanState[Cursor] {
	if state == nil {
		return nil
	}
	return &ScanState[Cursor]{
		Next:            clonePointer(state.Next),
		HasMoreUpstream: state.HasMoreUpstream,
	}
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
