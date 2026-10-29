package cold_test

import (
	"time"

	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"github.com/RedHuang-0622/seelex/kernel/task"
	"testing"
)

func TestSessionColdTaskRestoresCompactionsAndRetainedWindow(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	storedCompactions := []model.ContextCompaction{{Version: 1, Reason: "stored"}}
	record := model.SessionRecord{
		Execution: model.SessionExecutionRecord{
			Task:          &model.TaskState{ContextCompactions: storedCompactions},
			Paused:        true,
			PausedGoalID:  "goal-1",
			StoppedByUser: true,
			Continuations: []model.ContinuationAttempt{{GoalID: "goal-1", RequestID: "req-1", Attempt: 1}},
			ContextDraft: &model.ContextDraftState{
				Items: []model.ContextDraftItem{{Seq: 2, Bytes: "pending"}}, Watermark: 1, Uncommitted: true,
			},
			MemoryUpdates: []model.MemoryUpdate{{Seq: 1, Epoch: 1, Kind: "add", Digest: "d", Bytes: "memory"}},
			ToolsUpdates:  []model.ToolsUpdate{{Seq: 2, Epoch: 1, Kind: "add", Digest: "d", SentinelBytes: "sentinel"}},
			ToolCatalog:   []model.ToolCatalogEntry{{Name: "read"}},
			ToolsEpoch:    1,
		},
		Projection: &model.TaskContextProjection{
			Checkpoint: model.TaskCheckpoint{CoversEventRange: model.EventRange{End: 9}},
		},
	}
	transcript := []model.TranscriptEvent{{Seq: 4}}
	var restored task_context.RestoredTaskState
	component := cold.NewTask(cold.TaskDeps{
		LoadCompactions: func(session_runtime.Location, string) ([]model.ContextCompaction, bool, error) {
			t.Fatal("record compactions 存在时不得读新址")
			return nil, false, nil
		},
		LatestUser: func([]model.Message) string { return "latest" },
		RetainedFrom: func(got []model.TranscriptEvent, compactions []model.ContextCompaction) int {
			if len(got) != 1 || len(compactions) != 1 || compactions[0].Reason != "stored" {
				t.Fatalf("retained args = %+v/%+v", got, compactions)
			}
			return 3
		},
		Restore: func(sessionID string, state task_context.RestoredTaskState) {
			restored = state
		},
		Reset: func() { t.Fatal("有 record 时不得 reset") },
	})

	component.RestoreLocked("session-a", location, record, transcript, false, true)

	if restored.TranscriptSeq != 9 || restored.ContextRetainedFrom != 3 ||
		len(restored.ContextCompactions) != 1 || restored.FallbackObjective != "latest" {
		t.Fatalf("restored task = %+v", restored)
	}
	if !restored.Continuation.Paused || restored.Continuation.PausedGoalID != "goal-1" ||
		!restored.Continuation.StoppedByUser || len(restored.Continuation.Continuations) != 1 {
		t.Fatalf("continuation state not restored: %+v", restored.Continuation)
	}
	if restored.Continuation.PausedAt.After(time.Now()) {
		t.Fatalf("zero pause timestamp restored as future time: %v", restored.Continuation.PausedAt)
	}
	if restored.ContextDraft.Watermark != 1 || !restored.ContextDraft.Uncommitted || len(restored.ContextDraft.Items) != 1 {
		t.Fatalf("context draft not restored: %+v", restored.ContextDraft)
	}
	if len(restored.MemoryUpdates) != 1 || len(restored.ToolsUpdates) != 1 ||
		len(restored.ToolCatalog) != 1 || restored.ToolsEpoch != 1 {
		t.Fatalf("context epochs not restored: %+v", restored)
	}
}

func TestSessionColdTaskLoadsCompactionsFromNewStore(t *testing.T) {
	loaded := []model.ContextCompaction{{Version: 2, Reason: "new"}}
	var got []model.ContextCompaction
	component := cold.NewTask(cold.TaskDeps{
		LoadCompactions: func(session_runtime.Location, string) ([]model.ContextCompaction, bool, error) {
			return loaded, true, nil
		},
		LatestUser: func([]model.Message) string { return "" },
		RetainedFrom: func([]model.TranscriptEvent, []model.ContextCompaction) int {
			t.Fatal("renumbered transcript 不得推导 retainedFrom")
			return 0
		},
		Restore: func(_ string, state task_context.RestoredTaskState) {
			got = state.ContextCompactions
		},
		Reset: func() {},
	})

	component.RestoreLocked("session-a", session_runtime.Location{}, model.SessionRecord{},
		[]model.TranscriptEvent{{Seq: 2}}, true, true)

	if len(got) != 1 || got[0].Reason != "new" {
		t.Fatalf("compactions = %+v", got)
	}
}

func TestSessionColdTaskResetsWithoutRecord(t *testing.T) {
	reset := false
	component := cold.NewTask(cold.TaskDeps{
		Reset: func() { reset = true },
	})

	component.RestoreLocked("session-a", session_runtime.Location{}, model.SessionRecord{}, nil, false, false)

	if !reset {
		t.Fatal("无 record 时必须 reset task state")
	}
}
