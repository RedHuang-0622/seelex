package session

import (
	"errors"
	"sync"
	"testing"
)

func TestDraftSubmissionTrimsAfterViewCheck(t *testing.T) {
	var mu sync.Mutex
	var materialized string
	notInView := errors.New("not in view")
	application := NewDraftSubmission(DraftSubmissionDeps{
		IsDraftTarget: func(string) bool { return true },
		Transition:    func() sync.Locker { return &mu },
		IsInView:      func(string) bool { return true },
		Materialize:   func(input string) error { materialized = input; return nil },
		NotInView:     notInView,
	})
	if err := application.Materialize("session-a", "  hello  "); err != nil {
		t.Fatal(err)
	}
	if materialized != "hello" {
		t.Fatalf("materialized = %q", materialized)
	}
	application.deps.IsInView = func(string) bool { return false }
	if err := application.Materialize("session-a", "hello"); !errors.Is(err, notInView) {
		t.Fatalf("error = %v", err)
	}
}

func TestResumeApplicationBranches(t *testing.T) {
	var transitionMu sync.Mutex
	hotAttach := false
	application := NewResume(ResumeDeps{
		TransitionForSession: func(string) sync.Locker { return &transitionMu },
		Snapshot: func(string) ResumeState {
			return ResumeState{Hot: true, Previous: "old"}
		},
		MarkViewIntent: func(string) {},
		HotAttach: func(string) error {
			hotAttach = true
			return nil
		},
	})
	if err := application.Resume(" session-a "); err != nil || !hotAttach {
		t.Fatalf("Resume = %v, hotAttach=%v", err, hotAttach)
	}
}

func TestRestoreRecoveryFallsBackToDraft(t *testing.T) {
	var mu sync.Mutex
	reset := false
	published := false
	application := NewRestoreRecovery(RestoreRecoveryDeps{
		Lock:                 func() sync.Locker { return &mu },
		ClearRestoringLocked: func(string) {},
		SnapshotLocked: func(string, uint64) RestoreRecoveryState {
			return RestoreRecoveryState{StillActive: true, Latest: true}
		},
		HotAttach:                 func(string) error { return errors.New("missing") },
		ResetDraft:                func() { reset = true },
		PublishViewSessionChanged: func() { published = true },
	})
	application.Handle("session-a", "previous", 1, nil)
	if !reset || !published {
		t.Fatalf("reset=%v published=%v", reset, published)
	}
}
