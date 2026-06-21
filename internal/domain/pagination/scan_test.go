package pagination

import (
	"context"
	"reflect"
	"testing"
)

func TestScanFiltersAcrossPagesAndResumesWithoutEmbeddingItems(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	fetch := func(_ context.Context, cursor *int) (Page[int, int], error) {
		offset := 0
		if cursor != nil {
			offset = *cursor
		}
		end := min(offset+2, len(items))
		next := end
		return Page[int, int]{
			Items:   items[offset:end],
			Next:    &next,
			HasMore: end < len(items),
		}, nil
	}
	advance := func(cursor *int, consumed int) *int {
		if cursor != nil {
			consumed += *cursor
		}
		return &consumed
	}
	first, err := Scan(context.Background(), ScanOptions[int, int]{
		Limit:    2,
		MaxPages: 5,
		Fetch:    fetch,
		Advance:  advance,
		Accept: func(value int) bool {
			return value%2 == 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 3}; !reflect.DeepEqual(first.Items, want) {
		t.Fatalf("first items = %#v, want %#v", first.Items, want)
	}
	if !first.HasMore || first.Next == nil || first.Next.Next == nil || *first.Next.Next != 3 {
		t.Fatalf("first continuation = %#v", first.Next)
	}

	second, err := Scan(context.Background(), ScanOptions[int, int]{
		Limit:    2,
		MaxPages: 5,
		State:    first.Next,
		Fetch:    fetch,
		Advance:  advance,
		Accept: func(value int) bool {
			return value%2 == 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{5}; !reflect.DeepEqual(second.Items, want) {
		t.Fatalf("second items = %#v, want %#v", second.Items, want)
	}
	if second.HasMore || second.Next != nil {
		t.Fatalf("second continuation = %#v", second.Next)
	}
}

func TestScanStopsAtPageBudgetAndReturnsContinuation(t *testing.T) {
	pageTwo := "page-two"
	pageThree := "page-three"
	fetch := func(_ context.Context, cursor *string) (Page[string, int], error) {
		switch {
		case cursor == nil:
			return Page[string, int]{Items: []int{1}, Next: &pageTwo, HasMore: true}, nil
		case *cursor == pageTwo:
			return Page[string, int]{Items: []int{2}, Next: &pageThree, HasMore: true}, nil
		case *cursor == pageThree:
			return Page[string, int]{Items: []int{3}, HasMore: false}, nil
		default:
			t.Fatalf("unexpected cursor %q", *cursor)
			return Page[string, int]{}, nil
		}
	}
	first, err := Scan(context.Background(), ScanOptions[string, int]{
		Limit:    5,
		MaxPages: 2,
		Fetch:    fetch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(first.Items, want) {
		t.Fatalf("first items = %#v, want %#v", first.Items, want)
	}
	if !first.HasMore || first.Next == nil || first.Next.Next == nil || *first.Next.Next != pageThree {
		t.Fatalf("first continuation = %#v", first.Next)
	}

	second, err := Scan(context.Background(), ScanOptions[string, int]{
		Limit:    5,
		MaxPages: 2,
		State:    first.Next,
		Fetch:    fetch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{3}; !reflect.DeepEqual(second.Items, want) {
		t.Fatalf("second items = %#v, want %#v", second.Items, want)
	}
	if second.HasMore || second.Next != nil {
		t.Fatalf("second continuation = %#v", second.Next)
	}
}
