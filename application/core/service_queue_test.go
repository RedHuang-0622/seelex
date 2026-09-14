package core

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// startBlockingChat 启动一个阻塞中的回合（返回可在测试里显式放开的引擎）。
// blockCh 预建：不受 runChat goroutine 起跑时序影响，测试可确定性放开首轮。
func startBlockingChat(t *testing.T) (*Service, *blockingEngine) {
	t.Helper()
	engine := &blockingEngine{fakeEngine: &fakeEngine{}, blockCh: make(chan struct{})}
	service := mustNew(t, Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	t.Cleanup(service.Shutdown)
	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatalf("Submit(first): %v", err)
	}
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.Chat.Running })
	return service, engine
}

// enqueueQueuedInputs 在运行中的会话里排队若干输入（按给定顺序）。
func enqueueQueuedInputs(t *testing.T, service *Service, inputs ...string) {
	t.Helper()
	for _, input := range inputs {
		if err := service.Submit(context.Background(), input); err != nil {
			t.Fatalf("Submit(%q): %v", input, err)
		}
	}
}

// TestReorderQueuedInputReordersVisibleQueue 调换排队顺序：可见投影
// （ChatState.InputQueue/QueuedCount）随位置变化，正在运行的回合不受影响。
func TestReorderQueuedInputReordersVisibleQueue(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second", "third", "fourth")

	before := service.Snapshot().Chat
	if got := before.InputQueue; len(got) != 3 || got[0] != "second" || got[2] != "fourth" {
		t.Fatalf("queued order before reorder = %v", got)
	}

	// second third fourth --(0 → 2)--> third fourth second
	if err := service.ReorderQueuedInput("", 0, 2); err != nil {
		t.Fatalf("ReorderQueuedInput(0,2): %v", err)
	}
	after := service.Snapshot().Chat
	want := []string{"third", "fourth", "second"}
	if len(after.InputQueue) != len(want) {
		t.Fatalf("queued order after reorder = %v, want %v", after.InputQueue, want)
	}
	for index, input := range want {
		if after.InputQueue[index] != input {
			t.Fatalf("queued order after reorder = %v, want %v", after.InputQueue, want)
		}
	}
	if after.QueuedCount != 3 {
		t.Fatalf("QueuedCount after reorder = %d, want 3", after.QueuedCount)
	}
	// 运行中的回合不受影响：running / request_id / started_at 原样。
	if !after.Running || after.RequestID != before.RequestID || !after.StartedAt.Equal(before.StartedAt) {
		t.Fatalf("running turn mutated by reorder: before=%+v after=%+v", before, after)
	}
}

// TestReorderQueuedInputDrivesPromotedBatchOrder 调换后的顺序决定下一轮
// （队列提升为单批输入）实际发送给模型的内容顺序。
func TestReorderQueuedInputDrivesPromotedBatchOrder(t *testing.T) {
	service, engine := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second", "third")
	if err := service.ReorderQueuedInput("", 1, 0); err != nil {
		t.Fatalf("ReorderQueuedInput(1,0): %v", err)
	}
	close(engine.blockCh) // 放开首轮 → 队列提升为下一轮

	promoted := waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		for _, message := range snapshot.Conversation {
			if message.Role == "user" && message.Content == "third\n---\nsecond" {
				return true
			}
		}
		return false
	})
	found := false
	for _, message := range promoted.Conversation {
		if message.Role == "user" && message.Content == "third\n---\nsecond" {
			found = true
		}
	}
	if !found {
		t.Fatal("promoted batch did not follow the reordered queue")
	}
}

// TestRecallQueuedInputPopsEntryAndReturnsText 撤回 = 出队 + 交还展示原文：
// 队列与计数同步收缩，原文原样返回（引擎侧载荷随条目丢弃）。
func TestRecallQueuedInputPopsEntryAndReturnsText(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second", "third")

	text, err := service.RecallQueuedInput("", 1)
	if err != nil {
		t.Fatalf("RecallQueuedInput(1): %v", err)
	}
	if text != "third" {
		t.Fatalf("recalled text = %q, want third", text)
	}
	chat := service.Snapshot().Chat
	if len(chat.InputQueue) != 1 || chat.InputQueue[0] != "second" || chat.QueuedCount != 1 {
		t.Fatalf("queue after recall = %v (count %d), want [second] (1)", chat.InputQueue, chat.QueuedCount)
	}
	if !chat.Running {
		t.Fatal("recall must not stop the running turn")
	}

	// 撤回的条目不会再出现在下一轮里。
	left, err := service.RecallQueuedInput("", 0)
	if err != nil || left != "second" {
		t.Fatalf("second recall = %q/%v, want second/nil", left, err)
	}
	chat = service.Snapshot().Chat
	if len(chat.InputQueue) != 0 || chat.QueuedCount != 0 {
		t.Fatalf("queue after draining = %v (count %d), want empty", chat.InputQueue, chat.QueuedCount)
	}
}

// TestQueueEditErrorSemantics 越界 / 非运行态 / 会话不存在都有明确错误。
func TestQueueEditErrorSemantics(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second")

	if err := service.ReorderQueuedInput("", 0, 1); !errors.Is(err, ErrQueueIndexOutOfRange) {
		t.Fatalf("reorder out of range = %v, want ErrQueueIndexOutOfRange", err)
	}
	if _, err := service.RecallQueuedInput("", 2); !errors.Is(err, ErrQueueIndexOutOfRange) {
		t.Fatalf("recall out of range = %v, want ErrQueueIndexOutOfRange", err)
	}
	if err := service.ReorderQueuedInput("no-such-session", 0, 0); !errors.Is(err, ErrQueueSessionNotFound) {
		t.Fatalf("unknown session = %v, want ErrQueueSessionNotFound", err)
	}

	// 非运行态：没有回合在跑就没有「排队中」的输入。先把队列排空，否则取消
	// 首轮会把残余输入提升成下一轮（同样是运行态）。
	if text, err := service.RecallQueuedInput("", 0); err != nil || text != "second" {
		t.Fatalf("drain queue = %q/%v, want second/nil", text, err)
	}
	service.CancelChat("")
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return !snapshot.Chat.Running })
	if err := service.ReorderQueuedInput("", 0, 0); !errors.Is(err, ErrQueueNotRunning) {
		t.Fatalf("reorder while idle = %v, want ErrQueueNotRunning", err)
	}
	if _, err := service.RecallQueuedInput("", 0); !errors.Is(err, ErrQueueNotRunning) {
		t.Fatalf("recall while idle = %v, want ErrQueueNotRunning", err)
	}
}

// TestQueueEditErrorsBeforeAnySession 进程还没有任何会话单元时，视图会话按
// 「没有回合在跑」拒绝，而不是「会话不存在」。
func TestQueueEditErrorsBeforeAnySession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if err := service.ReorderQueuedInput("", 0, 1); !errors.Is(err, ErrQueueNotRunning) {
		t.Fatalf("reorder on a fresh process = %v, want ErrQueueNotRunning", err)
	}
	if _, err := service.RecallQueuedInput("", 0); !errors.Is(err, ErrQueueNotRunning) {
		t.Fatalf("recall on a fresh process = %v, want ErrQueueNotRunning", err)
	}
}

// TestConcurrentRecallQueuedInput（-race）并发撤回同一会话：撤回与 Enqueue /
// 提升共用 Core.ViewMu，因此每个条目只可能被撤回一次。
func TestConcurrentRecallQueuedInput(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "one", "two", "three")

	var (
		group    sync.WaitGroup
		mu       sync.Mutex
		succeed  int
		rejected int
	)
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			text, err := service.RecallQueuedInput("", 0)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				if text == "" {
					t.Error("recalled empty text")
				}
				succeed++
			case errors.Is(err, ErrQueueIndexOutOfRange):
				rejected++
			default:
				t.Errorf("unexpected recall error: %v", err)
			}
		}()
	}
	group.Wait()

	if succeed != 3 || rejected != 5 {
		t.Fatalf("concurrent recall = %d succeeded / %d rejected, want 3/5", succeed, rejected)
	}
	chat := service.Snapshot().Chat
	if len(chat.InputQueue) != 0 || chat.QueuedCount != 0 {
		t.Fatalf("queue after concurrent recall = %v (count %d), want empty", chat.InputQueue, chat.QueuedCount)
	}
}
