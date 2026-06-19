package pagination

import (
	"context"
	"reflect"
	"testing"
)

func TestScanUsesPendingBeforeFetching(t *testing.T) {
	state := &ScanState[string, int]{
		Pending: []int{1, 2},
	}
	calls := 0
	result, err := Scan(context.Background(), ScanOptions[string, int]{
		Limit:    1,
		MaxPages: 5,
		State:    state,
		Fetch: func(context.Context, *string) (Page[string, int], error) {
			calls++
			return Page[string, int]{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("fetch calls = %d, want 0", calls)
	}
	if want := []int{1}; !reflect.DeepEqual(result.Items, want) {
		t.Fatalf("items = %#v, want %#v", result.Items, want)
	}
	if !result.HasMore || result.Next == nil || !reflect.DeepEqual(result.Next.Pending, []int{2}) {
		t.Fatalf("continuation = %#v", result.Next)
	}
}

func TestScanFiltersAcrossPagesAndResumesWithOverflow(t *testing.T) {
	pageTwo := "page-two"
	fetch := func(_ context.Context, cursor *string) (Page[string, int], error) {
		if cursor == nil {
			return Page[string, int]{
				Items:   []int{1, 2},
				Next:    &pageTwo,
				HasMore: true,
			}, nil
		}
		if *cursor != pageTwo {
			t.Fatalf("cursor = %q, want %q", *cursor, pageTwo)
		}
		return Page[string, int]{
			Items:   []int{3, 4, 5},
			HasMore: false,
		}, nil
	}
	first, err := Scan(context.Background(), ScanOptions[string, int]{
		Limit:    2,
		MaxPages: 5,
		Fetch:    fetch,
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
	if !first.HasMore || first.Next == nil || !reflect.DeepEqual(first.Next.Pending, []int{5}) {
		t.Fatalf("first continuation = %#v", first.Next)
	}

	second, err := Scan(context.Background(), ScanOptions[string, int]{
		Limit:    2,
		MaxPages: 5,
		State:    first.Next,
		Fetch:    fetch,
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
