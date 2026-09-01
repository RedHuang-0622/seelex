package session

import (
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestThreadIsolation 并发写两个会话的队列互不串行、互不污染
// （暴力测试：约束 C2 线程隔离的单元级前置）。
func TestThreadIsolation(t *testing.T) {
	a, errA := NewSessionUnit("session-a")
	b, errB := NewSessionUnit("session-b")
	if errA != nil || errB != nil {
		t.Fatalf("NewSessionUnit: %v %v", errA, errB)
	}

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
	a, errA := NewSessionUnit("session-a")
	b, errB := NewSessionUnit("session-b")
	if errA != nil || errB != nil {
		t.Fatalf("NewSessionUnit: %v %v", errA, errB)
	}

	a.View.Conversation = append(a.View.Conversation, model.Message{ID: "m-a", Role: "assistant", Content: "A"})
	a.View.Chat = model.ChatState{Running: true, RequestID: "req-a"}
	b.View.Conversation = append(b.View.Conversation, model.Message{ID: "m-b", Role: "user", Content: "B"})

	if len(a.View.Conversation) != 1 || a.View.Conversation[0].ID != "m-a" {
		t.Fatalf("A view = %+v", a.View.Conversation)
	}
	if len(b.View.Conversation) != 1 || b.View.Conversation[0].ID != "m-b" {
		t.Fatalf("B view = %+v", b.View.Conversation)
	}
	if b.View.Chat.Running {
		t.Fatal("B chat state leaked from A")
	}
}

// TestDomainActivePointer V 指针只影响视图，不触碰任何会话单元内容。
func TestDomainActivePointer(t *testing.T) {
	domain := NewDomain()
	defer domain.Close()
	a, errA := NewSessionUnit("session-a")
	b, errB := NewSessionUnit("session-b")
	if errA != nil || errB != nil {
		t.Fatalf("NewSessionUnit: %v %v", errA, errB)
	}
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
	parent, err := NewSessionUnit("parent")
	if err != nil {
		t.Fatal(err)
	}
	parent.View.Conversation = append(parent.View.Conversation, model.Message{
		ID: "p-1", Role: "assistant", Content: "parent content",
	})
	parent.View.TotalMessages = 1
	parent.Enqueue(QueuedRequest{DisplayInput: "p-queued"})

	// fork 的容器级深拷贝（继承面：View/队列全新副本，不复用父切片/通道）。
	child, err := NewSessionUnit("child")
	if err != nil {
		t.Fatal(err)
	}
	child.View.Conversation = append([]model.Message(nil), parent.View.Conversation...)
	child.View.TotalMessages = parent.View.TotalMessages
	child.SetRequests(append([]QueuedRequest(nil), parent.PendingRequests()...))

	// 修改子会话：追加消息 + 追加队列项。
	child.View.Conversation = append(child.View.Conversation, model.Message{
		ID: "c-1", Role: "assistant", Content: "child mutation",
	})
	child.View.TotalMessages++
	child.Enqueue(QueuedRequest{DisplayInput: "c-queued"})

	// 父会话不受影响：消息数、内容、队列全部保持原状。
	if len(parent.View.Conversation) != 1 || parent.View.Conversation[0].ID != "p-1" {
		t.Fatalf("parent conversation mutated by child: %+v", parent.View.Conversation)
	}
	if parent.View.TotalMessages != 1 {
		t.Fatalf("parent total messages = %d, want 1", parent.View.TotalMessages)
	}
	pending := parent.PendingRequests()
	if len(pending) != 1 || pending[0].DisplayInput != "p-queued" {
		t.Fatalf("parent queue mutated by child: %+v", pending)
	}
}
