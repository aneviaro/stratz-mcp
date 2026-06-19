package batch

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
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

func TestRunJobsCancelsRemainingJobsAfterFailure(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	jobs := []Job[int]{
		func(context.Context) (int, error) {
			<-started
			return 0, errors.New("boom")
		},
		func(ctx context.Context) (int, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return 0, ctx.Err()
		},
	}
	_, err := RunJobs(context.Background(), jobs)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("RunJobs error = %v, want boom", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("RunJobs did not cancel the remaining job")
	}
}

func TestRunJobsEnforcesSharedRequestBudget(t *testing.T) {
	budget, err := stratz.NewRequestBudget(5)
	if err != nil {
		t.Fatal(err)
	}
	var charged atomic.Int32
	jobs := make([]Job[int], 6)
	for index := range jobs {
		index := index
		jobs[index] = func(context.Context) (int, error) {
			if !budget.Take() {
				return 0, &stratz.Error{
					Code:      contracts.ErrorCodeRequestBudgetExceeded,
					Message:   "The upstream request budget is exhausted",
					Details:   map[string]any{},
					Retryable: false,
				}
			}
			charged.Add(1)
			return index, nil
		}
	}
	_, err = RunJobs(context.Background(), jobs)
	var upstreamErr *stratz.Error
	if !errors.As(err, &upstreamErr) || upstreamErr.Code != contracts.ErrorCodeRequestBudgetExceeded {
		t.Fatalf("RunJobs error = %v, want request budget exceeded", err)
	}
	if charged.Load() != 5 || budget.Used() != 5 || budget.Remaining() != 0 {
		t.Fatalf("budget accounting = charged:%d used:%d remaining:%d", charged.Load(), budget.Used(), budget.Remaining())
	}
}
