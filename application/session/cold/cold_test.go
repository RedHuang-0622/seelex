package cold

import (
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestReadUsesRecordAndTranscriptAndSkipsLegacyHistory(t *testing.T) {
	historyLoaded := false
	application := NewRead(ReadDeps{
		RecoverTail: func(string) string { return "notice" },
		Locate:      func(string) session_runtime.Location { return session_runtime.Location{WorkspaceID: "workspace-a"} },
		LoadRecord: func(session_runtime.Location, string) (model.SessionRecord, int, bool, error) {
			return model.SessionRecord{Title: model.SessionTitle{Value: "stored"}}, 3, true, nil
		},
		LoadTranscript: func(session_runtime.Location, string) ([]model.TranscriptEvent, error) {
			return []model.TranscriptEvent{{Seq: 1}}, nil
		},
		LoadHistory: func(session_runtime.Location) ([]contract.EngineMessage, int, error) {
			historyLoaded = true
			return nil, 0, nil
		},
	})
	result, err := application.Read("session-a")
	if err != nil {
		t.Fatal(err)
	}
	if result.PendingTailNotice != "notice" || !result.HasRecord || result.RecordTotal != 3 ||
		len(result.Transcript) != 1 || historyLoaded {
		t.Fatalf("result = %+v, historyLoaded=%v", result, historyLoaded)
	}
}

func TestWorkspaceApplicationUnbindsOnlyWithoutWorkspace(t *testing.T) {
	var calls []string
	application := NewWorkspace(WorkspaceDeps{
		WorkspaceAvailable:    func() bool { return true },
		BindProjectRootIfSafe: func(string, string) bool { calls = append(calls, "root"); return true },
		SetWriteScope:         func(string) { calls = append(calls, "scope") },
		BindSession:           func(string, string) { calls = append(calls, "session") },
		AnyChatRunning:        func() bool { return false },
		UnbindGlobalRoot:      func() { calls = append(calls, "unbind-root") },
		UnbindSession:         func(string) { calls = append(calls, "unbind-session") },
		UnbindRuntimeRoot:     func(string) { calls = append(calls, "runtime-root") },
	})
	application.Bind("session-a", "workspace-a", &model.WorkspaceInfo{ID: "workspace-a", RootPath: "/repo"})
	if len(calls) != 3 || calls[0] != "root" || calls[1] != "scope" || calls[2] != "session" {
		t.Fatalf("bound calls = %+v", calls)
	}
	calls = nil
	application.Bind("session-a", "", nil)
	if len(calls) != 4 || calls[0] != "unbind-root" || calls[3] != "runtime-root" {
		t.Fatalf("unbound calls = %+v", calls)
	}
}
