package session

import (
	"errors"
	"sync"
	"testing"
)

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
