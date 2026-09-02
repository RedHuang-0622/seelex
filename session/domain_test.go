package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

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

// TestDomainConcurrentCommands（-race）：注册/注销/移动视图指针与读取并发进行，
// 断言命令同步返回即对所有 goroutine 可见（异步会让事件投递端的视图归属和
// Unit 查询读到旧状态）。
func TestDomainConcurrentCommands(t *testing.T) {
	domain := NewDomain()
	var group sync.WaitGroup

	for index := 0; index < 8; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			unit, err := NewSessionUnit(fmt.Sprintf("session-%d", index))
			if err != nil {
				t.Errorf("NewSessionUnit: %v", err)
				return
			}
			for round := 0; round < 40; round++ {
				domain.Register(unit)
				if domain.Unit(unit.ID) == nil {
					t.Errorf("register not visible right after return: %s", unit.ID)
					return
				}
				domain.SetActive(unit.ID)
				if got := domain.ActiveID(); got != "" && !isTestSessionID(got) {
					t.Errorf("active id = %q, want a registered session id", got)
				}
				domain.Remove(unit.ID)
				if domain.Unit(unit.ID) != nil {
					t.Errorf("remove not visible right after return: %s", unit.ID)
					return
				}
			}
		}(index)
	}
	group.Wait()
	if domain.Live() != 0 {
		t.Fatalf("live units after every worker removed its own = %d, want 0", domain.Live())
	}

	// 关闭阶段：并发且重复的 Close 必须幂等不 panic，停机后的调用不得挂起。
	stopped := make(chan struct{})
	for index := 0; index < 3; index++ {
		go func() {
			domain.Close()
			domain.Close()
			stopped <- struct{}{}
		}()
	}
	for range 3 {
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not return for every caller")
		}
	}
	if got := domain.Unit("session-0"); got != nil {
		t.Fatalf("Unit after Close = %v, want nil (call must not block or resurrect state)", got)
	}
}

// isTestSessionID 报告 id 是否形如本测试注册的 "session-<n>"。
func isTestSessionID(id string) bool {
	const prefix = "session-"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return false
	}
	for _, r := range id[len(prefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
