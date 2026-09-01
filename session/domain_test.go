package session

import (
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestLifecycleStateMachine 覆盖合法/非法迁移（边界测试：约束 C1 状态机）。
func TestLifecycleStateMachine(t *testing.T) {
	unit := NewUnit("session-a")
	if got := unit.Lifecycle(); got != StateCold {
		t.Fatalf("initial state = %q, want cold", got)
	}

	// 非法：COLD → LIVE / COLD → COLD
	if err := unit.Transition(StateLive); err == nil {
		t.Fatal("cold → live must be rejected (cold_load must pass prepared)")
	}
	if err := unit.Transition(StateCold); err == nil {
		t.Fatal("cold → cold must be rejected")
	}

	// 合法：COLD → PREPARED → LIVE → LIVE（fg/bg 平移）→ COLD（unload）
	if err := unit.Transition(StatePrepared); err != nil {
		t.Fatalf("cold → prepared: %v", err)
	}
	if err := unit.Transition(StateLive); err != nil {
		t.Fatalf("prepared → live: %v", err)
	}
	if err := unit.Transition(StateLive); err != nil {
		t.Fatalf("live → live (hot_attach): %v", err)
	}
	if err := unit.Transition(StateCold); err != nil {
		t.Fatalf("live → cold (unload): %v", err)
	}
}

// TestThreadIsolation 并发写两个会话的队列互不串行、互不污染
// （暴力测试：约束 C2 线程隔离的单元级前置）。
func TestThreadIsolation(t *testing.T) {
	a := NewUnit("session-a").Chat
	b := NewUnit("session-b").Chat

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(index int) {
			defer wg.Done()
			a.Enqueue(QueuedRequest{DisplayInput: "a"})
		}(i)
		go func(index int) {
			defer wg.Done()
			b.Enqueue(QueuedRequest{DisplayInput: "b"})
		}(i)
	}
	wg.Wait()

	if got := len(a.PendingRequests()); got != 50 {
		t.Fatalf("session-a queue = %d, want 50", got)
	}
	if got := len(b.PendingRequests()); got != 50 {
		t.Fatalf("session-b queue = %d, want 50", got)
	}
	for _, request := range a.PendingRequests() {
		if request.DisplayInput != "a" {
			t.Fatalf("session-a queue polluted with %q", request.DisplayInput)
		}
	}
}

// TestViewIsolation 两个会话的可见投影互不影响。
func TestViewIsolation(t *testing.T) {
	a := NewUnit("session-a").View
	b := NewUnit("session-b").View

	a.Conversation = append(a.Conversation, model.Message{ID: "m-a", Role: "assistant", Content: "A"})
	a.Chat = model.ChatState{Running: true, RequestID: "req-a"}
	b.Conversation = append(b.Conversation, model.Message{ID: "m-b", Role: "user", Content: "B"})

	if len(a.Conversation) != 1 || a.Conversation[0].ID != "m-a" {
		t.Fatalf("A view = %+v", a.Conversation)
	}
	if len(b.Conversation) != 1 || b.Conversation[0].ID != "m-b" {
		t.Fatalf("B view = %+v", b.Conversation)
	}
	if b.Chat.Running {
		t.Fatal("B chat state leaked from A")
	}
}

// TestDomainActivePointer V 指针只影响视图，不触碰任何会话单元内容。
func TestDomainActivePointer(t *testing.T) {
	domain := NewDomain()
	a := NewUnit("session-a")
	b := NewUnit("session-b")
	domain.Register(a)
	domain.Register(b)

	domain.SetActive("session-a")
	if got := domain.ActiveID(); got != "session-a" {
		t.Fatalf("active = %q, want session-a", got)
	}
	if domain.Live() != 2 {
		t.Fatalf("live units = %d, want 2", domain.Live())
	}
	domain.SetActive("session-b")
	if got := domain.ActiveID(); got != "session-b" {
		t.Fatalf("active = %q, want session-b", got)
	}
	// V 指针移动不改写任何单元
	if a.View != nil && len(a.View.Conversation) != 0 {
		t.Fatal("V pointer move mutated session-a view")
	}
	domain.Remove("session-a")
	if domain.Unit("session-a") != nil {
		t.Fatal("removed unit still registered")
	}
	if domain.Live() != 1 {
		t.Fatalf("live units = %d, want 1", domain.Live())
	}
}

// TestForkDeepCopyIsolation（S0）：fork 继承面 = 深拷贝——子会话 View/队列
// 的变更不得影响父会话（引用不相交）。容器层不变量；持久化侧由
// session_runtime/fork_test.go 覆盖（截断/物理复制）。
func TestForkDeepCopyIsolation(t *testing.T) {
	parent := NewUnit("parent")
	parent.View.Conversation = append(parent.View.Conversation, model.Message{
		ID: "p-1", Role: "assistant", Content: "parent content",
	})
	parent.View.TotalMessages = 1
	parent.Chat.Enqueue(QueuedRequest{DisplayInput: "p-queued"})

	// fork 的容器级深拷贝（继承面：View/队列全新副本，不复用父切片/通道）。
	child := NewUnit("child")
	child.View.Conversation = append([]model.Message(nil), parent.View.Conversation...)
	child.View.TotalMessages = parent.View.TotalMessages
	child.Chat.SetRequests(append([]QueuedRequest(nil), parent.Chat.PendingRequests()...))

	// 修改子会话：追加消息 + 追加队列项。
	child.View.Conversation = append(child.View.Conversation, model.Message{
		ID: "c-1", Role: "assistant", Content: "child mutation",
	})
	child.View.TotalMessages++
	child.Chat.Enqueue(QueuedRequest{DisplayInput: "c-queued"})

	// 父会话不受影响：消息数、内容、队列全部保持原状。
	if len(parent.View.Conversation) != 1 || parent.View.Conversation[0].ID != "p-1" {
		t.Fatalf("parent conversation mutated by child: %+v", parent.View.Conversation)
	}
	if parent.View.TotalMessages != 1 {
		t.Fatalf("parent total messages = %d, want 1", parent.View.TotalMessages)
	}
	pending := parent.Chat.PendingRequests()
	if len(pending) != 1 || pending[0].DisplayInput != "p-queued" {
		t.Fatalf("parent queue mutated by child: %+v", pending)
	}
}
