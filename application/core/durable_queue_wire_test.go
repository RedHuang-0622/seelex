package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// durable queue 应用侧接线（A1b）的接线证明：
//   - 运行中接受的输入 → 镜像落盘（QueueEnqueueWorkspace）；
//   - 回合收尾把整批提升为下一轮 → 标记消费（QueueMarkConsumedWorkspace，
//     turnID = 下一轮的 requestID = 该轮 message 行的 TaskID）；
//   - 该轮快照落盘成功 → 确认出队（QueueConfirmConsumedWorkspace）；
//   - 快照落盘失败 → 内容回草稿（QueueFailConsumedWorkspace）；
//   - 冷加载恢复 → 回填可见队列 + 下一次提交时一并提升（不自动开轮）。

// queueRecordingSessions 是 Dependencies.Sessions 的测试双件：在 base
// SessionPort 之上实现 durable queue 写入侧端口（SessionQueuePort），按真实
// 调用序列记录参数。
type queueRecordingSessions struct {
	fakeSessions
	mu        sync.Mutex
	enqueued  []string
	marked    []string
	confirmed []string
	failed    []string
	resent    []sessionstore.QueueItem
}

func (s *queueRecordingSessions) QueueEnqueueWorkspace(_, _, _, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enqueued = append(s.enqueued, content)
	return nil
}

func (s *queueRecordingSessions) QueueMarkConsumedWorkspace(_, _, turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = append(s.marked, turnID)
	return nil
}

func (s *queueRecordingSessions) QueueConfirmConsumedWorkspace(_, _, turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirmed = append(s.confirmed, turnID)
	return nil
}

func (s *queueRecordingSessions) QueueFailConsumedWorkspace(_, _, turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, turnID)
	return nil
}

func (s *queueRecordingSessions) QueueRecoverItemsWorkspace(_, _ string) (sessionstore.QueueRecoveryReport, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sessionstore.QueueRecoveryReport{Resent: append([]sessionstore.QueueItem(nil), s.resent...)}, true, nil
}

func (s *queueRecordingSessions) queueCalls() (enqueued, marked, confirmed, failed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.enqueued...), append([]string(nil), s.marked...),
		append([]string(nil), s.confirmed...), append([]string(nil), s.failed...)
}

// snapshotFailingSessions 让原子快照落盘失败，触发 persist 失败路径。
// 必须同时实现 SessionSnapshotPort 的两个方法：能力断言要求整组方法齐备，
// 只实现 Workspace 变体会断言失败而退回"无落盘"分支（本测试曾因此假绿）。
type snapshotFailingSessions struct{ queueRecordingSessions }

func (snapshotFailingSessions) SaveSessionSnapshot(
	string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult,
) error {
	return errors.New("disk unavailable")
}

func (snapshotFailingSessions) SaveSessionSnapshotWorkspace(
	string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult,
) error {
	return errors.New("disk unavailable")
}

func waitQueueCalls(t *testing.T, sessions interface {
	queueCalls() (enqueued, marked, confirmed, failed []string)
}, ready func(enqueued, marked, confirmed, failed []string) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		enqueued, marked, confirmed, failed := sessions.queueCalls()
		if ready(enqueued, marked, confirmed, failed) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	enqueued, marked, confirmed, failed := sessions.queueCalls()
	t.Fatalf("durable queue calls not observed: enqueued=%v marked=%v confirmed=%v failed=%v",
		enqueued, marked, confirmed, failed)
}

func newBlockingEngine() *sessionBackedBlockingEngine {
	return &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
}

// TestDurableQueueMirrorsQueuedInputAndConfirmsTurn 运行中提交 → 镜像落盘；
// 回合收尾 → 标记下一轮消费 + 确认本轮（快照已发布）。
func TestDurableQueueMirrorsQueuedInputAndConfirmsTurn(t *testing.T) {
	engine := newBlockingEngine()
	sessions := &queueRecordingSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.Submit(context.Background(), "第一轮输入"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("第一轮未进入引擎")
	}
	// 会话运行中：本条输入进内存队列 + durable 镜像（先队列后草稿）。
	if err := service.Submit(context.Background(), "排队第二问"); err != nil {
		t.Fatal(err)
	}
	waitQueueCalls(t, sessions, func(enqueued, _, _, _ []string) bool {
		return len(enqueued) == 1 && enqueued[0] == "排队第二问"
	})

	close(engine.release) // 放行第一轮 → 收尾提升排队输入

	// 收尾必须：确认第一轮（快照已发布）+ 把排队项标记为下一轮消费。
	waitQueueCalls(t, sessions, func(_, marked, confirmed, _ []string) bool {
		return len(marked) == 1 && len(confirmed) >= 1
	})
	_, marked, confirmed, failed := sessions.queueCalls()
	if marked[0] == "" {
		t.Fatalf("marked turnID 为空：%v", marked)
	}
	if failed := failed; len(failed) != 0 {
		t.Fatalf("快照成功时不应回草稿：%v", failed)
	}
	if confirmed[0] == "" {
		t.Fatalf("confirm turnID 为空：%v", confirmed)
	}
}

// TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure 快照落盘失败
// → 本轮消费项内容回草稿（不留"已消费但永不重发"的死条目）。
func TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure(t *testing.T) {
	engine := newBlockingEngine()
	sessions := &snapshotFailingSessions{queueRecordingSessions{}}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.Submit(context.Background(), "第一轮输入"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("第一轮未进入引擎")
	}
	close(engine.release)

	waitQueueCalls(t, sessions, func(_, _, _, failed []string) bool { return len(failed) >= 1 })
	_, _, confirmed, failed := sessions.queueCalls()
	if len(confirmed) != 0 {
		t.Fatalf("快照失败时不应确认出队：%v", confirmed)
	}
	if failed[0] == "" {
		t.Fatalf("fail turnID 为空：%v", failed)
	}
}

// TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit 冷加载恢复的输入
// 必须可见（不丢），并在下一次提交时随本轮一并提升（不自动开轮、不静默计费）。
func TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &queueRecordingSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	sessionID := service.Snapshot().Session.ID

	service.reEnqueueRecoveredInputs(sessionID, []sessionstore.QueueItem{
		{Seq: 1, Content: "重启恢复的输入", State: string("queued")},
	})
	snapshot := service.Snapshot()
	if snapshot.Chat.QueuedCount != 1 {
		t.Fatalf("QueuedCount = %d, want 1（恢复输入必须可见）", snapshot.Chat.QueuedCount)
	}
	if len(snapshot.Chat.InputQueue) != 1 || snapshot.Chat.InputQueue[0] != "重启恢复的输入" {
		t.Fatalf("InputQueue = %v, want [重启恢复的输入]", snapshot.Chat.InputQueue)
	}

	if err := service.Submit(context.Background(), "新消息"); err != nil {
		t.Fatal(err)
	}
	// 非运行分支：队列遗留项与新输入合并为同一轮。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		engine.mu.Lock()
		inputs := append([]string(nil), engine.chatInputs...)
		engine.mu.Unlock()
		for _, input := range inputs {
			if strings.Contains(input, "重启恢复的输入") && strings.Contains(input, "新消息") {
				drained := service.Snapshot()
				if drained.Chat.QueuedCount != 0 {
					t.Fatalf("提升后 QueuedCount = %d, want 0", drained.Chat.QueuedCount)
				}
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("恢复输入未随下一次提交一并提升发送")
}
