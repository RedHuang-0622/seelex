package assembly

import (
	"errors"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"sync"
	"testing"
)

func TestSessionRestoreRecoveryClearsRestoringAndIgnoresStaleView(t *testing.T) {
	var calls []string
	locker := &recordingLocker{}
	recovery := applicationsession.NewRestoreRecovery(applicationsession.RestoreRecoveryDeps{
		Lock: func() sync.Locker { return locker },
		ClearRestoringLocked: func(sessionID string) {
			calls = append(calls, "clear:"+sessionID)
		},
		SnapshotLocked: func(sessionID string, epoch uint64) applicationsession.RestoreRecoveryState {
			return applicationsession.RestoreRecoveryState{StillActive: false, Latest: true}
		},
		HotAttach: func(string) error {
			t.Fatal("非当前视图不得回退")
			return nil
		},
		ResetDraft: func() {
			t.Fatal("非当前视图不得重置草稿")
		},
		PublishViewSessionChanged: func() {
			t.Fatal("非当前视图不得发布视图通知")
		},
	})

	recovery.Handle("session-b", "session-a", 7, errors.New("failed"))

	if len(calls) != 1 || calls[0] != "clear:session-b" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestSessionRestoreRecoveryIgnoresSupersededEpoch(t *testing.T) {
	var published bool
	locker := &recordingLocker{}
	recovery := applicationsession.NewRestoreRecovery(applicationsession.RestoreRecoveryDeps{
		Lock:                 func() sync.Locker { return locker },
		ClearRestoringLocked: func(string) {},
		SnapshotLocked: func(string, uint64) applicationsession.RestoreRecoveryState {
			return applicationsession.RestoreRecoveryState{StillActive: true, Latest: false}
		},
		HotAttach: func(string) error {
			t.Fatal("旧 epoch 不得回退")
			return nil
		},
		ResetDraft: func() {
			t.Fatal("旧 epoch 不得重置")
		},
		PublishViewSessionChanged: func() { published = true },
	})

	recovery.Handle("session-b", "session-a", 7, errors.New("failed"))

	if published {
		t.Fatal("旧 epoch 不得发布视图通知")
	}
}

func TestSessionRestoreRecoveryFallsBackToPreviousSession(t *testing.T) {
	var calls []string
	locker := &recordingLocker{}
	recovery := applicationsession.NewRestoreRecovery(applicationsession.RestoreRecoveryDeps{
		Lock:                 func() sync.Locker { return locker },
		ClearRestoringLocked: func(string) {},
		SnapshotLocked: func(string, uint64) applicationsession.RestoreRecoveryState {
			return applicationsession.RestoreRecoveryState{StillActive: true, Latest: true}
		},
		HotAttach: func(previousID string) error {
			calls = append(calls, "hot:"+previousID)
			return nil
		},
		ResetDraft: func() {
			calls = append(calls, "draft")
		},
		PublishViewSessionChanged: func() {
			calls = append(calls, "publish")
		},
	})

	recovery.Handle("session-b", "session-a", 7, errors.New("failed"))

	if len(calls) != 2 || calls[0] != "hot:session-a" || calls[1] != "publish" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestSessionRestoreRecoveryResetsDraftWhenNoFallbackExists(t *testing.T) {
	tests := []struct {
		name       string
		previousID string
		hotErr     error
	}{
		{name: "no-previous"},
		{name: "hot-failed", previousID: "session-a", hotErr: errors.New("hot failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			locker := &recordingLocker{}
			recovery := applicationsession.NewRestoreRecovery(applicationsession.RestoreRecoveryDeps{
				Lock:                 func() sync.Locker { return locker },
				ClearRestoringLocked: func(string) {},
				SnapshotLocked: func(string, uint64) applicationsession.RestoreRecoveryState {
					return applicationsession.RestoreRecoveryState{StillActive: true, Latest: true}
				},
				HotAttach: func(string) error { return test.hotErr },
				ResetDraft: func() {
					calls = append(calls, "draft")
				},
				PublishViewSessionChanged: func() {
					calls = append(calls, "publish")
				},
			})

			recovery.Handle("session-b", test.previousID, 7, errors.New("failed"))

			if len(calls) != 2 || calls[0] != "draft" || calls[1] != "publish" {
				t.Fatalf("calls = %v", calls)
			}
		})
	}
}
