package sessionstate

import (
	"sync"
	"testing"
	"time"
)

// TestSessionUnitComponents（T2.1）：元组契约 S_i=(id,K,parent,E,V,Q,C,B,status)。
func TestSessionUnitComponents(t *testing.T) {
	unit, err := NewSessionUnit("sess-1",
		WithKind(KindSubagent),
		WithParent("sess-main"),
		WithTitle("节点 A"),
	)
	if err != nil {
		t.Fatalf("NewSessionUnit: %v", err)
	}

	// id / K / parent
	if unit.ID != "sess-1" {
		t.Fatalf("id = %q, want sess-1", unit.ID)
	}
	if unit.Kind != KindSubagent {
		t.Fatalf("K = %q, want subagent", unit.Kind)
	}
	if unit.ParentID != "sess-main" {
		t.Fatalf("parent = %q, want sess-main", unit.ParentID)
	}
	if unit.Binding.Kind != KindSubagent || unit.Binding.ParentID != "sess-main" {
		t.Fatalf("binding = %+v, want kind=subagent parent=sess-main", unit.Binding)
	}

	// E / V / Q / C 骨架非空
	if unit.Engine != nil {
		t.Fatal("engine handle must be nil before cold load")
	}
	if unit.View == nil || unit.Queue == nil || unit.Context == nil {
		t.Fatal("V/Q/C must be non-nil in the unit skeleton")
	}
	// 队列 seq 有序、会话隔离
	unit.Queue.Enqueue("first", nil)
	unit.Queue.Enqueue("second", nil)
	snapshot := unit.Queue.Snapshot()
	if len(snapshot) != 2 || snapshot[0].Seq != 1 || snapshot[1].Seq != 2 {
		t.Fatalf("queue snapshot = %+v, want seq 1,2", snapshot)
	}
	other := NewInputQueue()
	other.Enqueue("other", nil)
	if unit.Queue.Len() != 2 {
		t.Fatalf("queue polluted by other session: len = %d", unit.Queue.Len())
	}
}

// TestEmptyAndNilHandles（B1）：空 id / nil 队列 / 空会话不 panic、明确错误。
func TestEmptyAndNilHandles(t *testing.T) {
	if _, err := NewSessionUnit(""); err == nil {
		t.Fatal("empty session ID must be rejected")
	}
	if _, err := NewSessionUnit("", WithTitle("x")); err == nil {
		t.Fatal("empty session ID with opts must be rejected")
	}

	var nilUnit *SessionUnit
	if nilUnit != nil {
		t.Fatal("nil unit sanity")
	}
	// nil 队列不 panic（零值 InputQueue 可用）
	var queue *InputQueue
	if queue != nil {
		t.Fatal("nil queue sanity")
	}
	if queue.Len() != 0 {
		t.Fatal("nil queue len must be 0")
	}
	if _, ok := queue.Dequeue(); ok {
		t.Fatal("nil queue dequeue must be empty")
	}

	// 空队列 Enqueue 后 Snapshot 正常
	empty := NewInputQueue()
	if empty.Len() != 0 || len(empty.Snapshot()) != 0 {
		t.Fatal("empty queue must have zero length")
	}
}

// TestLockOrdering（R2）：Transition→Domain→Unit→View 锁序下并发迁移 +
// 注册表操作无死锁（超时门禁），视图读写不受会话迁移阻塞。
func TestLockOrdering(t *testing.T) {
	domain := NewDomain()
	defer domain.Close()
	units := make([]*SessionUnit, 8)
	for index := 0; index < len(units); index++ {
		unit, err := NewSessionUnit(string(rune('a' + index)))
		if err != nil {
			t.Fatalf("NewSessionUnit: %v", err)
		}
		units[index] = unit
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for index := 0; index < len(units); index++ {
			unit := units[index]
			wg.Add(2)
			go func() {
				defer wg.Done()
				for round := 0; round < 100; round++ {
					unit.SetRequests([]QueuedRequest{{DisplayInput: "queued"}})
					unit.AddApproval("approval")
					unit.RemoveApproval("approval")
					unit.View.Mutate(func(view *View) { view.Revision++ })
				}
			}()
			go func() {
				defer wg.Done()
				for round := 0; round < 100; round++ {
					domain.Register(&SessionUnit{ID: unit.ID, View: &View{}, Queue: NewInputQueue(), Context: &ContextStack{}})
					domain.Remove(unit.ID)
					domain.SetActive(unit.ID)
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
		// 通过：并发迁移/注册/视图读写无死锁
	case <-time.After(5 * time.Second):
		t.Fatal("lock ordering deadlock (timeout)")
	}
}
