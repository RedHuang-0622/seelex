package assembly

import (
	"errors"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"strings"
	"sync"
	"testing"
)

func TestSessionDraftEntryRejectsClosedAndDraining(t *testing.T) {
	tests := []struct {
		name  string
		state applicationsession.DraftEntryState
		want  error
	}{
		{name: "closed", state: applicationsession.DraftEntryState{Closed: true}, want: errors.New("application is shut down")},
		{name: "draining", state: applicationsession.DraftEntryState{Draining: true}, want: ErrApplicationDraining},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			locker := &recordingLocker{}
			component := applicationsession.NewDraftEntry(applicationsession.DraftEntryDeps{
				Transition:    func() sync.Locker { return locker },
				Snapshot:      func() applicationsession.DraftEntryState { return test.state },
				DrainingError: ErrApplicationDraining,
			})
			err := component.Begin()
			if test.name == "closed" {
				if err == nil || err.Error() != test.want.Error() {
					t.Fatalf("Begin error = %v, want %v", err, test.want)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("Begin error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSessionDraftEntryIsIdempotentForDraft(t *testing.T) {
	locker := &recordingLocker{}
	component := applicationsession.NewDraftEntry(applicationsession.DraftEntryDeps{
		Transition: func() sync.Locker { return locker },
		Snapshot: func() applicationsession.DraftEntryState {
			return applicationsession.DraftEntryState{Draft: true}
		},
		ClearSkill: func() {
			t.Fatal("已经处于草稿时不得清理状态")
		},
	})

	if err := component.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
}

func TestSessionDraftEntryStopsWhenIdleSessionSaveFails(t *testing.T) {
	saveErr := errors.New("disk full")
	locker := &recordingLocker{}
	component := applicationsession.NewDraftEntry(applicationsession.DraftEntryDeps{
		Transition: func() sync.Locker { return locker },
		Snapshot: func() applicationsession.DraftEntryState {
			return applicationsession.DraftEntryState{
				SessionID: "session-a", WorkspaceID: "workspace-a",
			}
		},
		HistoryLen: func(string) int { return 2 },
		PersistCurrent: func(sessionID, workspaceID string) error {
			if sessionID != "session-a" || workspaceID != "workspace-a" {
				t.Fatalf("persist target = %s/%s", sessionID, workspaceID)
			}
			return saveErr
		},
		ClearSkill: func() {
			t.Fatal("保存失败后不得清理 Skill")
		},
	})

	err := component.Begin()

	if !errors.Is(err, saveErr) {
		t.Fatalf("Begin error = %v, want save error", err)
	}
}

func TestSessionDraftEntryPreservesRunningSessionAndPublishesDraft(t *testing.T) {
	workspace := &model.WorkspaceInfo{ID: "workspace-a", RootPath: "C:/workspace-a"}
	locker := &recordingLocker{}
	var calls []string
	component := applicationsession.NewDraftEntry(applicationsession.DraftEntryDeps{
		Transition: func() sync.Locker { return locker },
		Snapshot: func() applicationsession.DraftEntryState {
			calls = append(calls, "snapshot")
			return applicationsession.DraftEntryState{
				SessionID: "session-a", WorkspaceID: "workspace-a", CurrentRunning: true,
			}
		},
		HistoryLen: func(string) int {
			calls = append(calls, "history")
			return 4
		},
		PersistCurrent: func(string, string) error {
			t.Fatal("运行中的旧会话不得保存或清理")
			return nil
		},
		ClearSkill: func() { calls = append(calls, "skill") },
		ClearIdleHistory: func(string) {
			t.Fatal("运行中的旧会话不得清历史或解绑 context")
		},
		DetachIdleContext: func() {
			t.Fatal("运行中的旧会话不得清历史或解绑 context")
		},
		PrepareDraftSlot: func() applicationsession.DraftSlot {
			calls = append(calls, "slot")
			return applicationsession.DraftSlot{ID: "draft-a", Workspace: workspace}
		},
		ReadTier: func(sessionID string) string {
			calls = append(calls, "tier:"+sessionID)
			return "auto"
		},
		ClearWorkspaceBindings: func() {
			t.Fatal("恢复的工作区草稿不得清 workspace binding")
		},
		EnterShell: func(slot applicationsession.DraftSlot, tier string) uint64 {
			calls = append(calls, "shell:"+slot.ID+":"+tier)
			return 23
		},
		PublishRuntimeProjections: func() { calls = append(calls, "runtime") },
		SwitchTasks:               func(sessionID string) { calls = append(calls, "tasks:"+sessionID) },
		ClearSubagentTree:         func() { calls = append(calls, "subagents") },
		RefreshWorkTable:          func() { calls = append(calls, "worktable") },
		PublishSnapshot: func(revision uint64, sessionID string) {
			calls = append(calls, "snapshot-event:"+sessionID)
			if revision != 23 {
				t.Fatalf("revision = %d, want 23", revision)
			}
		},
	})

	if err := component.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	want := []string{
		"snapshot",
		"skill",
		"slot",
		"tier:draft-a",
		"shell:draft-a:auto",
		"runtime",
		"tasks:draft-a",
		"subagents",
		"worktable",
		"snapshot-event:draft-a",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("transition calls = %q, want lock,unlock", got)
	}
}
