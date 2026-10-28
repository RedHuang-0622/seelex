package integration

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"sync"
	"testing"
	"time"
)

type draftSnapshotSessions struct {
	fakeSessions
	mu    sync.Mutex
	calls [][]model.TranscriptEvent
}

func (sessions *draftSnapshotSessions) AppendDraftSnapshotWorkspace(
	_ string,
	_ string,
	events []model.TranscriptEvent,
) (int, bool, error) {
	copied := append([]model.TranscriptEvent(nil), events...)
	sessions.mu.Lock()
	sessions.calls = append(sessions.calls, copied)
	sessions.mu.Unlock()
	return len(copied), true, nil
}

func (sessions *draftSnapshotSessions) snapshotCalls() [][]model.TranscriptEvent {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	calls := make([][]model.TranscriptEvent, len(sessions.calls))
	for index := range sessions.calls {
		calls[index] = append([]model.TranscriptEvent(nil), sessions.calls[index]...)
	}
	return calls
}

// TestDraftSnapshotAdvancesLeftPointerByMemorySuffix 钉住双指针语义：
// 内存 transcript 只有显式快照时才写；写成功后 left 推进，下一次只发送
// 新增后缀，不重复发送已快照行。
func TestDraftSnapshotAdvancesLeftPointerByMemorySuffix(t *testing.T) {
	sessions := &draftSnapshotSessions{}
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	sessionID := service.components.frontendSnapshot.Snapshot().Session.ID
	const requestID = "draft-snapshot-turn"

	service.ViewMu.Lock()
	user := service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: requestID, Role: "user", Content: "long task",
	})
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: requestID, Role: "assistant", Content: "step one",
	})
	service.ViewMu.Unlock()

	left, found := service.draftSnapshotBaseSeq(sessionID, requestID)
	if !found || left != user.Seq-1 {
		t.Fatalf("draft base = %d/%v, want %d/true", left, found, user.Seq-1)
	}
	if calls := sessions.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("高频内存追加阶段发生了 %d 次 draft 写盘，want 0", len(calls))
	}

	right, supported, err := service.persistDraftSnapshotSuffix(sessionID, left)
	if err != nil || !supported {
		t.Fatalf("first snapshot: supported=%v err=%v", supported, err)
	}
	calls := sessions.snapshotCalls()
	if len(calls) != 1 || len(calls[0]) != 2 {
		t.Fatalf("first snapshot calls = %+v, want one call with two rows", calls)
	}
	if calls[0][0].Seq != user.Seq || calls[0][1].Seq != right {
		t.Fatalf("first snapshot seqs = [%d,%d], want [%d,%d]",
			calls[0][0].Seq, calls[0][1].Seq, user.Seq, right)
	}

	if current, supported, err := service.persistDraftSnapshotSuffix(sessionID, right); err != nil || !supported || current != right {
		t.Fatalf("empty snapshot moved pointer: current=%d supported=%v err=%v", current, supported, err)
	}
	if calls = sessions.snapshotCalls(); len(calls) != 1 {
		t.Fatalf("empty snapshot issued another write: %d calls", len(calls))
	}

	service.ViewMu.Lock()
	tool := service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: requestID, Role: "tool", Content: "step result",
	})
	service.ViewMu.Unlock()
	next, supported, err := service.persistDraftSnapshotSuffix(sessionID, right)
	if err != nil || !supported || next != tool.Seq {
		t.Fatalf("incremental snapshot = %d/%v/%v, want %d/true/nil", next, supported, err, tool.Seq)
	}
	calls = sessions.snapshotCalls()
	if len(calls) != 2 || len(calls[1]) != 1 || calls[1][0].Seq != tool.Seq {
		t.Fatalf("incremental write = %+v, want only seq %d", calls, tool.Seq)
	}
}

// TestDraftSnapshotLoopIsPeriodicAndStops 覆盖长任务定时器：回合运行中按周期
// 写一次后缀，stop 返回后不再产生草稿 I/O。
func TestDraftSnapshotLoopIsPeriodicAndStops(t *testing.T) {
	previous := draftSnapshotInterval
	draftSnapshotInterval = 5 * time.Millisecond
	t.Cleanup(func() { draftSnapshotInterval = previous })

	sessions := &draftSnapshotSessions{}
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	sessionID := service.components.frontendSnapshot.Snapshot().Session.ID
	const requestID = "draft-snapshot-loop"
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: requestID, Role: "user", Content: "long task",
	})
	service.ViewMu.Unlock()

	stop := service.startDraftSnapshotLoop(t.Context(), sessionID, requestID)
	deadline := time.Now().Add(2 * time.Second)
	for len(sessions.snapshotCalls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls := sessions.snapshotCalls(); len(calls) == 0 {
		stop()
		t.Fatal("periodic snapshot did not run")
	}
	stop()
	count := len(sessions.snapshotCalls())
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: requestID, Role: "assistant", Content: "after stop",
	})
	service.ViewMu.Unlock()
	time.Sleep(2 * draftSnapshotInterval)
	if got := len(sessions.snapshotCalls()); got != count {
		t.Fatalf("snapshot loop wrote after stop: before=%d after=%d", count, got)
	}
}
