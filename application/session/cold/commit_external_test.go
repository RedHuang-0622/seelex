package cold_test

import (
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

func TestSessionColdCommitActivePath(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	unit.SetChatState(model.ChatState{RequestID: "turn-a"}, nil)
	snapshot := model.Snapshot{
		Session:     model.SessionState{ID: "session-a"},
		Interaction: &model.Interaction{ID: "approval-a"},
	}
	workspace := &model.WorkspaceInfo{ID: "workspace-a"}
	projection := switchworkspace.Projection{}
	var calls []string
	commit := cold.NewCommit(cold.CommitDeps{
		AppendPendingTail: func(sessionID, notice string) { calls = append(calls, "tail:"+notice) },
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			calls = append(calls, "chat:"+chat.RequestID)
		},
		MirrorPlan: func(sessionID string, plan *model.PlanState) {
			calls = append(calls, "plan:"+plan.Name)
		},
		SystemPrompt: func() string {
			calls = append(calls, "prompt")
			return "system prompt"
		},
		ApplyWorkspace: func(got *model.WorkspaceInfo, gotProjection switchworkspace.Projection) {
			calls = append(calls, "workspace:"+got.ID)
		},
		BumpRevision: func() uint64 {
			calls = append(calls, "revision")
			return 17
		},
	})
	record := model.SessionRecord{
		PlanStack:    []model.SessionPlanFrame{{Plan: &model.PlanState{Name: "active plan"}}},
		ActivePlanID: "",
	}

	got := commit.CommitLocked("session-a", true, true, record, unit, workspace, projection, "tail notice", &snapshot)

	if got.Revision != 17 || got.SystemPrompt != "system prompt" {
		t.Fatalf("commit result = %+v", got)
	}
	if snapshot.Interaction != nil || snapshot.CurrentWorkspace != workspace {
		t.Fatalf("snapshot commit = %+v", snapshot)
	}
	want := []string{"tail:tail notice", "chat:turn-a", "plan:active plan", "prompt", "workspace:workspace-a", "revision"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestSessionColdCommitNonActiveDoesNotTouchSharedFacts(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Interaction: &model.Interaction{ID: "keep"}}
	var calls []string
	commit := cold.NewCommit(cold.CommitDeps{
		AppendPendingTail: func(string, string) { calls = append(calls, "tail") },
		SetSessionChat:    func(string, model.ChatState) { calls = append(calls, "chat") },
		MirrorPlan:        func(string, *model.PlanState) { t.Fatal("非 active 不得镜像 plan") },
		SystemPrompt:      func() string { return "prompt" },
		ApplyWorkspace:    func(*model.WorkspaceInfo, switchworkspace.Projection) { t.Fatal("非 active 不得应用 workspace") },
		BumpRevision:      func() uint64 { t.Fatal("非 active 不得 bump revision"); return 0 },
	})

	got := commit.CommitLocked("session-a", false, true, model.SessionRecord{}, unit, &model.WorkspaceInfo{ID: "workspace-a"}, switchworkspace.Projection{}, "", &snapshot)

	if got.Revision != 0 || snapshot.Interaction == nil || snapshot.CurrentWorkspace != nil {
		t.Fatalf("non-active commit = %+v snapshot=%+v", got, snapshot)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
}
