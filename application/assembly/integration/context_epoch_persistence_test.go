//go:build !extended

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

func TestSessionRecordPersistsContinuationDraftMemoryAndTools(t *testing.T) {
	engine := &fakeEngine{}
	service := newLegacyTestService(t, engine)
	defer service.components.lifecycleApplication.Shutdown()
	sessionID := engine.SessionID()
	now := time.Now()

	service.components.tasks.UpdateContinuationStateFor(sessionID, func(state *model.SessionContinuationState) {
		state.Paused = true
		state.PausedAt = now
		state.PausedGoalID = "goal-1"
		state.StoppedByUser = true
		state.StoppedAt = now
		state.Continuations = []model.ContinuationAttempt{{
			At: now, GoalID: "goal-1", RequestID: "req-1", Attempt: 1,
		}}
	})
	service.components.tasks.ReplaceContextDraftStateFor(sessionID, model.ContextDraftState{
		Items:       []model.ContextDraftItem{{Seq: 7, Bytes: "wire", Final: true}},
		Watermark:   6,
		Uncommitted: true,
	})
	if _, err := service.components.tasks.AppendContextMemoryFor(
		sessionID, 3, task_context.MemoryUpdateAdd, "memory:3", "frozen", "epoch-freeze",
	); err != nil {
		t.Fatal(err)
	}
	if changed, epoch := service.components.tasks.ReplaceToolCatalogFor(sessionID, []model.ToolCatalogEntry{
		{Name: "read", Description: "read files"},
	}); !changed || epoch != 1 {
		t.Fatalf("freeze catalog changed=%t epoch=%d", changed, epoch)
	}
	sentinel := task_context.RenderToolsSentinel(1, []string{"read"}, nil, nil)
	if _, err := service.components.tasks.AppendContextToolsFor(
		sessionID, 1, task_context.ToolsUpdateAdd, []string{"read"}, sentinel, "catalog-freeze",
	); err != nil {
		t.Fatal(err)
	}

	service.ViewMu.Lock()
	record := service.components.sessions.SessionRecordLocked(sessionID, nil)
	service.ViewMu.Unlock()
	execution := record.Execution
	if !execution.Paused || execution.PausedGoalID != "goal-1" || !execution.StoppedByUser ||
		len(execution.Continuations) != 1 {
		t.Fatalf("continuation fields not persisted: %+v", execution)
	}
	if execution.ContextDraft == nil || execution.ContextDraft.Watermark != 6 ||
		!execution.ContextDraft.Uncommitted || len(execution.ContextDraft.Items) != 1 {
		t.Fatalf("draft fields not persisted: %+v", execution.ContextDraft)
	}
	if len(execution.MemoryUpdates) != 1 || execution.MemoryUpdates[0].Bytes != "frozen" {
		t.Fatalf("memory rows not persisted: %+v", execution.MemoryUpdates)
	}
	if execution.ToolsEpoch != 1 || len(execution.ToolCatalog) != 1 || len(execution.ToolsUpdates) != 1 {
		t.Fatalf("tool epoch not persisted: %+v", execution)
	}
	sum := sha256.Sum256([]byte(sentinel))
	if execution.ToolsUpdates[0].Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("tool digest mismatch: %q", execution.ToolsUpdates[0].Digest)
	}
}
