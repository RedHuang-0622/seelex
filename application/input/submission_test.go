package input

import (
	"context"
	"errors"
	"testing"
)

func TestSubmissionApplicationPreservesLifecycleAndDispatch(t *testing.T) {
	closedErr := errors.New("closed")
	drainingErr := errors.New("draining")
	dispatched := ""
	var closed, draining bool
	application := NewSubmission(SubmissionDeps{
		Lifecycle:     func() (bool, bool) { return closed, draining },
		ShutdownError: closedErr,
		DrainingError: drainingErr,
		Dispatch: func(_ context.Context, input string) error {
			dispatched = input
			return nil
		},
	})

	closed = true
	if err := application.Submit(context.Background(), "ignored"); !errors.Is(err, closedErr) {
		t.Fatalf("closed err = %v", err)
	}
	closed, draining = false, true
	if err := application.Submit(context.Background(), "ignored"); !errors.Is(err, drainingErr) {
		t.Fatalf("draining err = %v", err)
	}
	closed, draining = false, false
	if err := application.Submit(context.Background(), "  hello  "); err != nil {
		t.Fatal(err)
	}
	if dispatched != "hello" {
		t.Fatalf("dispatch = %q", dispatched)
	}
}
