package service

import (
	"errors"
	"testing"
)

func TestCombinePostCommitActionsRunsInOrder(t *testing.T) {
	calls := make([]string, 0, 2)
	action := combinePostCommitActions(
		func() error {
			calls = append(calls, "first")
			return nil
		},
		func() error {
			calls = append(calls, "second")
			return nil
		},
	)

	if action == nil {
		t.Fatal("expected combined action")
	}
	if err := action(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("unexpected call order: %#v", calls)
	}
}

func TestCombinePostCommitActionsStopsOnFirstError(t *testing.T) {
	want := errors.New("boom")
	calls := 0
	action := combinePostCommitActions(
		func() error {
			calls++
			return want
		},
		func() error {
			calls++
			return nil
		},
	)

	err := action()
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped error %v, got %v", want, err)
	}
	if calls != 1 {
		t.Fatalf("expected second action not to run, calls=%d", calls)
	}
}
