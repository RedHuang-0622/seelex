package session

import (
	"errors"
	"sync"
	"testing"

	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

func TestDeleteApplicationOrdersRecordReleaseAndPublication(t *testing.T) {
	var mu sync.RWMutex
	var calls []string
	application := NewDelete(DeleteDeps{
		ViewMu: &mu,
		DeleteRecord: func(string) error {
			calls = append(calls, "delete")
			return nil
		},
		ReleaseAsync:  func(string) { calls = append(calls, "release-async") },
		IsViewSession: func(string) bool { return true },
		ReleaseMemory: func(string) error {
			calls = append(calls, "release-memory")
			return nil
		},
		HasWorkspace:    func() bool { return false },
		SwitchDraft:     func() { calls = append(calls, "draft") },
		BumpRevision:    func() uint64 { calls = append(calls, "bump"); return 3 },
		PublishSnapshot: func(uint64, string) { calls = append(calls, "publish") },
		RefreshCatalog:  func() { calls = append(calls, "catalog") },
	})
	if err := application.Delete("session-a"); err != nil {
		t.Fatal(err)
	}
	if got := len(calls); got != 7 || calls[0] != "delete" || calls[1] != "release-async" ||
		calls[2] != "release-memory" || calls[6] != "catalog" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestDeleteApplicationStopsOnRecordError(t *testing.T) {
	want := errors.New("delete")
	application := NewDelete(DeleteDeps{
		ViewMu:       &sync.RWMutex{},
		DeleteRecord: func(string) error { return want },
		ReleaseAsync: func(string) { t.Fatal("release ran after record error") },
	})
	if err := application.Delete("session-a"); !errors.Is(err, want) {
		t.Fatalf("Delete error = %v", err)
	}
}

func TestDeleteApplicationAppliesWorkspaceProjection(t *testing.T) {
	var mu sync.RWMutex
	var applied bool
	application := NewDelete(DeleteDeps{
		ViewMu:          &mu,
		DeleteRecord:    func(string) error { return nil },
		ReleaseAsync:    func(string) {},
		IsViewSession:   func(string) bool { return false },
		ReleaseMemory:   func(string) error { return nil },
		HasWorkspace:    func() bool { return true },
		UnbindWorkspace: func(string) {},
		WorkspaceProjection: func() switchworkspace.Projection {
			return switchworkspace.Projection{}
		},
		ApplyWorkspace:  func(switchworkspace.Projection) { applied = true },
		SwitchDraft:     func() {},
		BumpRevision:    func() uint64 { return 1 },
		PublishSnapshot: func(uint64, string) {},
		RefreshCatalog:  func() {},
	})
	if err := application.Delete("session-a"); err != nil || !applied {
		t.Fatalf("Delete = %v, applied=%v", err, applied)
	}
}
