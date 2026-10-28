package session_test

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"sync"
	"testing"
)

func TestSessionRestoreDraftResetPublishesDraftShell(t *testing.T) {
	locker := &recordingLocker{}
	var calls []string
	reset := applicationsession.NewRestoreDraftReset(applicationsession.RestoreDraftResetDeps{
		Lock: func() sync.Locker { return locker },
		EnsureSlotLocked: func() string {
			if locker.locked != 1 {
				t.Fatalf("EnsureSlotLocked called with %d locks", locker.locked)
			}
			calls = append(calls, "slot")
			return "draft-a"
		},
		EnterShellLocked: func(draftID string) uint64 {
			if locker.locked != 1 || draftID != "draft-a" {
				t.Fatalf("EnterShellLocked = %q with %d locks", draftID, locker.locked)
			}
			calls = append(calls, "shell")
			return 31
		},
		PublishRuntimeProjections: func() {
			if locker.locked != 0 {
				t.Fatalf("runtime publish called with %d locks", locker.locked)
			}
			calls = append(calls, "runtime")
		},
		PublishSnapshot: func(revision uint64, draftID string) {
			if locker.locked != 0 || revision != 31 || draftID != "draft-a" {
				t.Fatalf("PublishSnapshot = %d/%q with %d locks", revision, draftID, locker.locked)
			}
			calls = append(calls, "snapshot")
		},
	})

	reset.Reset()

	want := []string{"slot", "shell", "runtime", "snapshot"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}
