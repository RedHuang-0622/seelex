package toolrecord

import (
	"errors"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

type fakeStore struct {
	saves     []string
	snapshots []dto.ToolRecordSnapshot
	loads     int
	persisted *dto.ToolRecordSnapshot
	saveErr   error
}

func (store *fakeStore) SaveToolRecordSnapshot(sessionID string, snapshot dto.ToolRecordSnapshot) error {
	if store.saveErr != nil {
		return store.saveErr
	}
	store.saves = append(store.saves, sessionID)
	store.snapshots = append(store.snapshots, snapshot)
	return nil
}

func (store *fakeStore) LoadToolRecordSnapshot(string) (dto.ToolRecordSnapshot, bool, error) {
	store.loads++
	if store.persisted == nil {
		return dto.ToolRecordSnapshot{}, false, nil
	}
	return *store.persisted, true, nil
}

func TestSetEnabledPersistsBeforeChangingMemory(t *testing.T) {
	saveErr := errors.New("disk full")
	var saved []bool
	application := New(Deps{SaveEnabled: func(enabled bool) error {
		saved = append(saved, enabled)
		return saveErr
	}})

	applied, err := application.SetEnabled(true)
	if !errors.Is(err, saveErr) || applied || application.Enabled() {
		t.Fatalf("failed save changed memory: applied=%v enabled=%v err=%v", applied, application.Enabled(), err)
	}

	application.mu.Lock()
	application.deps.SaveEnabled = func(enabled bool) error {
		saved = append(saved, enabled)
		return nil
	}
	application.mu.Unlock()
	applied, err = application.SetEnabled(true)
	if err != nil || !applied || !application.Enabled() {
		t.Fatalf("successful save not applied: applied=%v enabled=%v err=%v", applied, application.Enabled(), err)
	}
	if len(saved) != 2 || saved[0] != true || saved[1] != true {
		t.Fatalf("save calls = %v", saved)
	}
}

func TestRecordOutcomeCountsSuccessFailureAndReasons(t *testing.T) {
	application := New(Deps{})
	application.ApplyEnabled(true)

	application.RecordOutcome("s1", "bash", false, "")
	application.RecordOutcome("s1", "read_file", true, "permission denied")
	application.RecordCommandOutcome("s1", "bash", 2)

	view := application.SessionView("s1")
	if !view.Exists || view.Total != 3 || view.Success != 1 || view.Failure != 2 {
		t.Fatalf("session view = %+v", view)
	}
	reasons := map[string]string{}
	for _, stat := range view.Tools {
		if len(stat.Reasons) > 0 {
			reasons[stat.Tool] = stat.Reasons[0].Reason
		}
	}
	if reasons["read_file"] != dto.ToolReasonPermission || reasons["bash"] != dto.ToolReasonCommandFailed {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestFlushDrainsStagesAndIsDirtyGated(t *testing.T) {
	store := &fakeStore{}
	application := New(Deps{WritableSession: func(string) bool { return true }})
	application.ApplyEnabled(true)
	application.SetSnapshotStore(store)

	application.StageUnitOutcome("s1", "subagent:node", "bash", true, "timeout")
	application.Flush("s1")
	if len(store.saves) != 1 || len(store.snapshots[0].Tools) != 1 {
		t.Fatalf("flush did not drain stage: %+v", store.snapshots)
	}
	application.Flush("s1")
	if len(store.saves) != 1 {
		t.Fatalf("clean accumulator rewrote snapshot: %d", len(store.saves))
	}
}

func TestFlushFailureAndBoardLazyLoad(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("disk full")}
	application := New(Deps{
		WritableSession: func(string) bool { return true },
		ViewSessionID:   func() string { return "s1" },
	})
	application.ApplyEnabled(true)
	application.SetSnapshotStore(store)
	application.RecordOutcome("s1", "bash", false, "")
	application.Flush("s1")
	if board := application.Board(); board.LedgerError != "disk full" {
		t.Fatalf("board ledger error = %q", board.LedgerError)
	}

	persisted := dto.ToolRecordSnapshot{At: time.Now().UTC(), Tools: []dto.ToolRecordStat{
		{Tool: "read_file", Total: 2, Success: 1, Failure: 1},
	}}
	application = New(Deps{ViewSessionID: func() string { return "s2" }})
	store = &fakeStore{persisted: &persisted}
	application.SetSnapshotStore(store)
	first := application.Board()
	second := application.Board()
	if first.Total != 2 || second.Total != 2 || store.loads != 1 {
		t.Fatalf("lazy load: first=%+v second=%+v loads=%d", first, second, store.loads)
	}
}

func TestOwnerDropsSeqIdempotencyAndRelease(t *testing.T) {
	application := New(Deps{})
	application.ApplyEnabled(true)
	application.StageUnitOutcome("", "subagent:node", "bash", true, "")
	if got := application.Diagnostics().DroppedReadings[DropNoOwner]; got != 1 {
		t.Fatalf("no-owner drops = %d", got)
	}

	stats := []dto.ToolRecordStat{{Tool: "bash", Total: 1, Success: 1}}
	application.CommitUnit("s1", "subagent:node", 3, stats)
	application.CommitUnit("s1", "subagent:node", 3, stats)
	application.CommitUnit("s1", "subagent:node", 2, stats)
	if view := application.SessionView("s1"); view.Total != 1 {
		t.Fatalf("seq idempotency failed: %+v", view)
	}

	application.StageUnitOutcome("s1", "role:r1", "read_file", true, "")
	application.ReleaseSession("s1")
	if view := application.SessionView("s1"); view.Exists || view.Total != 0 {
		t.Fatalf("released session remains: %+v", view)
	}
	if diagnostics := application.Diagnostics(); diagnostics.UnitCount != 0 {
		t.Fatalf("released stage remains: %+v", diagnostics)
	}
	application.CommitUnit("s1", "subagent:node", 1, stats)
	if view := application.SessionView("s1"); view.Total != 1 {
		t.Fatalf("reused session ID must accept seq 1: %+v", view)
	}
}

func TestDisabledRecordsAndStagesNothing(t *testing.T) {
	store := &fakeStore{}
	application := New(Deps{WritableSession: func(string) bool { return true }})
	application.SetSnapshotStore(store)
	application.StageUnitOutcome("s1", "subagent:node", "bash", true, "")
	application.RecordOutcome("s1", "bash", true, "")
	application.Flush("s1")

	if view := application.SessionView("s1"); view.Exists {
		t.Fatalf("disabled ledger created state: %+v", view)
	}
	if diagnostics := application.Diagnostics(); diagnostics.UnitCount != 0 {
		t.Fatalf("disabled ledger created a stage: %+v", diagnostics)
	}
	if len(store.saves) != 0 {
		t.Fatalf("disabled ledger wrote snapshots: %v", store.saves)
	}
}

func TestUnitIDAndStatusHelpers(t *testing.T) {
	if got := SubagentUnitID("", "event-1"); got != "subagent:event-1" {
		t.Fatalf("subagent event fallback = %q", got)
	}
	if got := RoleUnitID("", "event-2"); got != "role:event-2" {
		t.Fatalf("role event fallback = %q", got)
	}
	if CountsStatus(dto.ToolEventRunning) || !CountsStatus(dto.ToolEventSuccess) || !CountsStatus(dto.ToolEventError) {
		t.Fatal("terminal status predicate drift")
	}
	if !TerminalSubagentStatus(dto.SubAgentInterrupted) || TerminalSubagentStatus(dto.SubAgentRunning) {
		t.Fatal("terminal subagent predicate drift")
	}
}
