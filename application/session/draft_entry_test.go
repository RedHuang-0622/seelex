package session

import (
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestDraftEntryOrdersIdleSessionCleanup(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	draining := errors.New("draining")
	application := NewDraftEntry(DraftEntryDeps{
		Transition: func() sync.Locker { return &mu },
		Snapshot: func() DraftEntryState {
			return DraftEntryState{SessionID: "old", WorkspaceID: "workspace-a"}
		},
		HistoryLen: func(string) int { return 2 },
		PersistCurrent: func(sessionID, workspaceID string) error {
			calls = append(calls, "persist:"+sessionID+":"+workspaceID)
			return nil
		},
		ClearIdleHistory:  func(string) { calls = append(calls, "history") },
		ClearSkill:        func() { calls = append(calls, "skill") },
		DetachIdleContext: func() { calls = append(calls, "detach") },
		PrepareDraftSlot: func() DraftSlot {
			calls = append(calls, "slot")
			return DraftSlot{ID: "draft-a"}
		},
		ReadTier:               func(string) string { return "manual" },
		ClearWorkspaceBindings: func() { calls = append(calls, "unbind") },
		EnterShell: func(slot DraftSlot, tier string) uint64 {
			calls = append(calls, "shell:"+slot.ID+":"+tier)
			return 8
		},
		PublishRuntimeProjections: func() { calls = append(calls, "runtime") },
		SwitchTasks:               func(string) { calls = append(calls, "tasks") },
		ClearSubagentTree:         func() { calls = append(calls, "subagents") },
		RefreshWorkTable:          func() { calls = append(calls, "worktable") },
		PublishSnapshot:           func(uint64, string) { calls = append(calls, "snapshot") },
		DrainingError:             draining,
	})
	if err := application.Begin(); err != nil {
		t.Fatal(err)
	}
	want := "persist:old:workspace-a,history,skill,detach,slot,unbind,shell:draft-a:manual,runtime,tasks,subagents,worktable,snapshot"
	if got := joinCalls(calls); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}

func TestDraftEntrySkipsPersistenceForRunningSession(t *testing.T) {
	var mu sync.Mutex
	persisted := false
	application := NewDraftEntry(DraftEntryDeps{
		Transition: func() sync.Locker { return &mu },
		Snapshot: func() DraftEntryState {
			return DraftEntryState{SessionID: "running", CurrentRunning: true}
		},
		HistoryLen: func(string) int { return 10 },
		PersistCurrent: func(string, string) error {
			persisted = true
			return nil
		},
		ClearIdleHistory:          func(string) { t.Fatal("running history cleared") },
		ClearSkill:                func() {},
		DetachIdleContext:         func() { t.Fatal("running context detached") },
		PrepareDraftSlot:          func() DraftSlot { return DraftSlot{ID: "draft-a"} },
		ReadTier:                  func(string) string { return "manual" },
		ClearWorkspaceBindings:    func() {},
		EnterShell:                func(DraftSlot, string) uint64 { return 1 },
		PublishRuntimeProjections: func() {},
		SwitchTasks:               func(string) {},
		ClearSubagentTree:         func() {},
		RefreshWorkTable:          func() {},
		PublishSnapshot:           func(uint64, string) {},
	})
	if err := application.Begin(); err != nil {
		t.Fatal(err)
	}
	if persisted {
		t.Fatal("running session persisted")
	}
}

func TestDraftMaterializationOrdersFirstInput(t *testing.T) {
	var calls []string
	application := NewDraftMaterialization(DraftMaterializationDeps{
		Snapshot: func() DraftMaterializationSource {
			return DraftMaterializationSource{
				Draft: true, DraftID: "draft-a",
				Workspace: &model.WorkspaceInfo{ID: "workspace-a", RootPath: "/repo"},
			}
		},
		BindWorkspace: func(*model.WorkspaceInfo) error { calls = append(calls, "bind"); return nil },
		OpenEngine: func(draftID string, _ *model.WorkspaceInfo) (string, error) {
			calls = append(calls, "open:"+draftID)
			return "session-a", nil
		},
		Title: func(input string) model.SessionTitle {
			calls = append(calls, "title:"+input)
			return model.SessionTitle{Value: input}
		},
		Commit: func(id string, title model.SessionTitle) uint64 {
			calls = append(calls, "commit:"+id+":"+title.Value)
			return 5
		},
		PublishRuntimeProjections: func() { calls = append(calls, "runtime") },
		PublishSnapshot:           func(uint64, string) { calls = append(calls, "snapshot") },
		RefreshCatalog:            func() { calls = append(calls, "catalog") },
		FlushToolRecord:           func(string) { calls = append(calls, "flush") },
		TouchResident:             func(string) { calls = append(calls, "resident") },
	})
	if err := application.Materialize("hello"); err != nil {
		t.Fatal(err)
	}
	want := "bind,open:draft-a,title:hello,commit:session-a:hello,runtime,snapshot,catalog,flush,resident"
	if got := joinCalls(calls); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}

func joinCalls(calls []string) string {
	result := ""
	for index, call := range calls {
		if index > 0 {
			result += ","
		}
		result += call
	}
	return result
}
