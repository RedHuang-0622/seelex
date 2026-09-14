package core

import (
	"context"
	"errors"
	"sync"
	"testing"

	selexsession "github.com/RedHuang-0622/seelex/session"
)

// enqueueRawQueuedInput 用会话域 API 直接排队一条非 chatRequest 载荷的输入：
// 域层 Payload 是 any（域不解释载荷），任何"投递即固化"的新载荷类型
// （控制指令 / 系统注入 / 未来的排队对象）都从这里进队列。它只用于暴露
// 「投影下标空间」与「队列下标空间」是否同一套。
func enqueueRawQueuedInput(t *testing.T, service *Service, text string) {
	t.Helper()
	unit := service.sessionUnitLocked(service.Snapshot().Session.ID)
	if unit == nil {
		t.Fatal("session unit missing")
	}
	unit.Enqueue(selexsession.QueuedRequest{DisplayInput: text, Payload: text})
}

// TestQueueEditIndexSpaceMatchesProjection（节点 156 红线）：ChatState.InputQueue
// 是用户看到、也是 GUI/TUI 回传的下标空间。若投影对载荷做类型过滤，投影会比
// 真实队列短——用户看到的第 i 行不是队列第 i 项，调换"看不见效果"、撤回把没
// 显示过的条目交还输入框。本测试断言投影与队列逐项对齐，且撤回/换序作用在
// 用户点的那一行上。
func TestQueueEditIndexSpaceMatchesProjection(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second")
	enqueueRawQueuedInput(t, service, "control")
	if err := service.Submit(context.Background(), "fourth"); err != nil {
		t.Fatalf("Submit(fourth): %v", err)
	}

	chat := service.Snapshot().Chat
	want := []string{"second", "control", "fourth"}
	if len(chat.InputQueue) != len(want) {
		t.Fatalf("投影长度 = %d (%v), want %d (%v)：投影下标空间必须与队列下标空间逐项对齐",
			len(chat.InputQueue), chat.InputQueue, len(want), want)
	}
	for index, input := range want {
		if chat.InputQueue[index] != input {
			t.Fatalf("投影 = %v, want %v", chat.InputQueue, want)
		}
	}
	if chat.QueuedCount != len(want) {
		t.Fatalf("QueuedCount = %d, want %d（计数口径必须与投影同源）", chat.QueuedCount, len(want))
	}

	// 撤回第 1 行 = 用户看到的 "control"。旧实现（按载荷过滤投影）会撤回队列
	// 第 1 项却把"没显示过的条目"交还输入框。
	text, err := service.RecallQueuedInput("", 1)
	if err != nil {
		t.Fatalf("RecallQueuedInput(1): %v", err)
	}
	if text != "control" {
		t.Fatalf("撤回第 1 行得到 %q, want %q（下标空间错位）", text, "control")
	}
	chat = service.Snapshot().Chat
	if len(chat.InputQueue) != 2 || chat.InputQueue[0] != "second" || chat.InputQueue[1] != "fourth" {
		t.Fatalf("撤回后投影 = %v, want [second fourth]", chat.InputQueue)
	}
}

// TestReorderQueuedInputFollowsProjectionIndex：调换的下标是投影下标——旧实现
// 下"调换用户看到的两行"可能出现"操作成功但列表不变"（移动的是被过滤掉的行）。
func TestReorderQueuedInputFollowsProjectionIndex(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second")
	enqueueRawQueuedInput(t, service, "control")

	// 用户看到 [second control]，把第 0 行移到第 1 行 → 期望 [control second]。
	if err := service.ReorderQueuedInput("", 0, 1); err != nil {
		t.Fatalf("ReorderQueuedInput(0,1): %v", err)
	}
	chat := service.Snapshot().Chat
	if len(chat.InputQueue) != 2 || chat.InputQueue[0] != "control" || chat.InputQueue[1] != "second" {
		t.Fatalf("投影 = %v, want [control second]：调换必须作用在用户看到的那一行", chat.InputQueue)
	}
}

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
