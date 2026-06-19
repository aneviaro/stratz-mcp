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

// ScanState preserves pending filtered items and the upstream continuation
// point for a later call.
type ScanState[Cursor any, Item any] struct {
	Pending         []Item  `json:"pending,omitempty"`
	Next            *Cursor `json:"next,omitempty"`
	HasMoreUpstream bool    `json:"has_more_upstream"`
}

// ScanOptions configures one bounded client-side scan pass.
type ScanOptions[Cursor any, Item any] struct {
	Limit    int
	MaxPages int
	State    *ScanState[Cursor, Item]
	Fetch    func(context.Context, *Cursor) (Page[Cursor, Item], error)
	Accept   func(Item) bool
}

// ScanResult contains the filtered items and any continuation state.
type ScanResult[Cursor any, Item any] struct {
	Items        []Item
	Next         *ScanState[Cursor, Item]
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
	drainPending(result, state, options.Limit)
	if len(result.Items) == options.Limit || (state != nil && len(state.Pending) == 0 && !state.HasMoreUpstream && state.Next == nil) {
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
		page, err := options.Fetch(ctx, clonePointer(next))
		if err != nil {
			return nil, err
		}
		result.PagesScanned++
		next = clonePointer(page.Next)
		hasMoreUpstream = page.HasMore
		if !page.HasMore {
			next = nil
		}

		filtered := make([]Item, 0, len(page.Items))
		for _, item := range page.Items {
			if options.Accept(item) {
				filtered = append(filtered, item)
			}
		}
		result.Items = append(result.Items, filtered...)
		if len(result.Items) > options.Limit {
			overflow := append([]Item(nil), result.Items[options.Limit:]...)
			result.Items = result.Items[:options.Limit]
			result.Next = normalizeState(&ScanState[Cursor, Item]{
				Pending:         overflow,
				Next:            clonePointer(next),
				HasMoreUpstream: hasMoreUpstream,
			})
			result.HasMore = result.Next != nil
			return result, nil
		}
		if len(result.Items) == options.Limit {
			result.Next = normalizeState(&ScanState[Cursor, Item]{
				Next:            clonePointer(next),
				HasMoreUpstream: hasMoreUpstream,
			})
			result.HasMore = result.Next != nil
			return result, nil
		}
		if !hasMoreUpstream {
			return result, nil
		}
	}

	result.Next = normalizeState(&ScanState[Cursor, Item]{
		Next:            clonePointer(next),
		HasMoreUpstream: hasMoreUpstream,
	})
	result.HasMore = result.Next != nil
	return result, nil
}

func drainPending[Cursor any, Item any](
	result *ScanResult[Cursor, Item],
	state *ScanState[Cursor, Item],
	limit int,
) {
	if result == nil || state == nil || limit <= 0 {
		return
	}
	remaining := limit - len(result.Items)
	if remaining <= 0 || len(state.Pending) == 0 {
		return
	}
	if len(state.Pending) <= remaining {
		result.Items = append(result.Items, state.Pending...)
		state.Pending = nil
		return
	}
	result.Items = append(result.Items, state.Pending[:remaining]...)
	state.Pending = append([]Item(nil), state.Pending[remaining:]...)
}

func normalizeState[Cursor any, Item any](
	state *ScanState[Cursor, Item],
) *ScanState[Cursor, Item] {
	if state == nil {
		return nil
	}
	state = cloneState(state)
	if len(state.Pending) == 0 && !state.HasMoreUpstream {
		state.Next = nil
	}
	if len(state.Pending) == 0 && state.Next == nil && !state.HasMoreUpstream {
		return nil
	}
	return state
}

func cloneState[Cursor any, Item any](
	state *ScanState[Cursor, Item],
) *ScanState[Cursor, Item] {
	if state == nil {
		return nil
	}
	return &ScanState[Cursor, Item]{
		Pending:         append([]Item(nil), state.Pending...),
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
