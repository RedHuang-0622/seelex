package cold_test

import (
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"testing"
)

func TestSessionColdViewInstallsRecordWindowAndActiveFacts(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	forkRef := &model.SessionForkRef{ParentSessionID: "parent"}
	record := model.SessionRecord{
		Execution: model.SessionExecutionRecord{
			ReadFiles: []model.ReadFileRef{{Path: "a.txt"}},
			Task:      &model.TaskState{RequestID: "turn-a", ContextCompactions: []model.ContextCompaction{{Reason: "old"}}},
		},
	}
	var installed *sessioncase.View
	var advanced bool
	component := cold.NewView(cold.ViewDeps{
		LoadForkRef: func(got session_runtime.Location, sessionID string) *model.SessionForkRef {
			if got != location {
				t.Fatalf("fork location = %+v", got)
			}
			return forkRef
		},
		AdvanceSeq: func(string, []model.Message) { advanced = true },
		RecordTail: func(model.SessionRecord, int) []model.Message {
			return []model.Message{{ID: "m1", Role: "user", Content: "saved"}}
		},
		ViewEmpty:        func(string) bool { return true },
		SetView:          func(_ string, view *sessioncase.View) { installed = view },
		AppendHistory:    func(string, []contract.EngineMessage) { t.Fatal("record path 不得 append legacy") },
		MutateView:       func(string, func(*sessioncase.View)) { t.Fatal("record path 不得修 legacy totals") },
		MirrorActiveView: func() {},
	})
	snapshot := model.Snapshot{}

	component.InstallLocked("session-a", location, record, true, nil, 0, 10, 2, 8, true, &snapshot)

	if installed == nil || !advanced {
		t.Fatalf("install=%v advanced=%v", installed != nil, advanced)
	}
	if installed.TotalMessages != 10 || installed.HistoryOffset != 2 || !installed.HasMoreHistory ||
		installed.ConversationWindow != 8 || len(installed.Conversation) != 2 ||
		installed.Conversation[0].Role != "system" || len(installed.ReadFiles) != 1 ||
		installed.ForkedFrom != forkRef {
		t.Fatalf("installed view = %+v", installed)
	}
	if snapshot.Session.ForkedFrom == nil || snapshot.Session.ForkedFrom.ParentSessionID != "parent" ||
		snapshot.Task == nil || snapshot.Task.RequestID != "turn-a" {
		t.Fatalf("snapshot facts = %+v task=%+v", snapshot.Session.ForkedFrom, snapshot.Task)
	}
}

func TestSessionColdViewKeepsNewerVisibleConversation(t *testing.T) {
	record := model.SessionRecord{}
	setCalled := false
	mirrored := false
	component := cold.NewView(cold.ViewDeps{
		LoadForkRef:      func(session_runtime.Location, string) *model.SessionForkRef { return nil },
		AdvanceSeq:       func(string, []model.Message) {},
		RecordTail:       func(model.SessionRecord, int) []model.Message { return nil },
		ViewEmpty:        func(string) bool { return false },
		SetView:          func(string, *sessioncase.View) { setCalled = true },
		AppendHistory:    func(string, []contract.EngineMessage) {},
		MutateView:       func(string, func(*sessioncase.View)) {},
		MirrorActiveView: func() { mirrored = true },
	})

	component.InstallLocked("session-a", session_runtime.Location{}, record, true, nil, 0, 0, 0, 1, false, &model.Snapshot{})

	if setCalled || mirrored {
		t.Fatalf("newer visible conversation must not be overwritten: set=%v mirrored=%v", setCalled, mirrored)
	}
}

func TestSessionColdViewAppendsLegacyAndFixesPaging(t *testing.T) {
	history := []contract.EngineMessage{{Role: "user", Content: "legacy"}}
	appended := false
	mutated := false
	mirrored := false
	component := cold.NewView(cold.ViewDeps{
		LoadForkRef: func(session_runtime.Location, string) *model.SessionForkRef { return nil },
		AdvanceSeq:  func(string, []model.Message) { t.Fatal("legacy path 不得 advance record seq") },
		RecordTail:  func(model.SessionRecord, int) []model.Message { return nil },
		ViewEmpty:   func(string) bool { return true },
		SetView:     func(string, *sessioncase.View) { t.Fatal("legacy path 使用 appendHistory") },
		AppendHistory: func(sessionID string, got []contract.EngineMessage) {
			appended = sessionID == "session-a" && len(got) == 1
		},
		MutateView: func(sessionID string, update func(*sessioncase.View)) {
			mutated = true
			view := &sessioncase.View{Conversation: []model.Message{{Role: "user", Content: "legacy"}}}
			update(view)
			if view.TotalMessages != 5 || view.HistoryOffset != 4 || !view.HasMoreHistory {
				t.Fatalf("legacy paging = %+v", view)
			}
		},
		MirrorActiveView: func() { mirrored = true },
	})

	component.InstallLocked("session-a", session_runtime.Location{}, model.SessionRecord{}, false, history, 5, 1, 0, 1, false, &model.Snapshot{})

	if !appended || !mutated || !mirrored {
		t.Fatalf("appended=%v mutated=%v mirrored=%v", appended, mutated, mirrored)
	}
}
