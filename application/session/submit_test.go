package session

import (
	"context"
	"errors"
	"testing"
)

func TestSubmitApplicationMaterializesAndRoutesExplicitSession(t *testing.T) {
	var calls []string
	loaded := false
	contentUnloaded := false
	application := NewSubmit(SubmitDeps{
		Materialize: func(sessionID, text string) error {
			calls = append(calls, "materialize:"+sessionID+":"+text)
			return nil
		},
		Loaded: func(sessionID string) (bool, bool) {
			calls = append(calls, "loaded:"+sessionID)
			return loaded, contentUnloaded
		},
		Resume: func(sessionID string) error {
			calls = append(calls, "resume:"+sessionID)
			return nil
		},
		Submit: func(_ context.Context, sessionID, text string) error {
			calls = append(calls, "submit:"+sessionID+":"+text)
			return nil
		},
	})

	if err := application.SubmitToSession(context.Background(), " s1 ", " hello "); err != nil {
		t.Fatal(err)
	}
	want := []string{"materialize:s1: hello ", "loaded:s1", "resume:s1", "submit:s1: hello "}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}

	calls = nil
	loaded, contentUnloaded = true, false
	if err := application.SubmitToSession(context.Background(), "s1", "next"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[1] != "loaded:s1" || calls[2] != "submit:s1:next" {
		t.Fatalf("loaded calls = %v", calls)
	}
}

func TestSubmitApplicationStopsOnMaterializeFailure(t *testing.T) {
	materializeErr := errors.New("materialize failed")
	application := NewSubmit(SubmitDeps{
		Materialize: func(string, string) error { return materializeErr },
	})
	if err := application.SubmitToSession(context.Background(), "s1", "text"); !errors.Is(err, materializeErr) {
		t.Fatalf("err = %v", err)
	}
	if err := application.SubmitToSession(context.Background(), " ", "text"); err == nil {
		t.Fatal("empty session ID must fail")
	}
}
