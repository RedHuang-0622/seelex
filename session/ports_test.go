package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	seelesession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// 编译期断言：EnginePort 契约可被具体实现满足（接口先行）。
var _ EnginePort = (*fakeEnginePort)(nil)

// fakeEnginePort 是 EnginePort 的测试桩（空实现，仅供编译期契约断言）。
type fakeEnginePort struct{}

func (fakeEnginePort) HasSession(string) bool { return false }
func (fakeEnginePort) NewMainSessionWithID(string, *seelesession.LoopHooks) (*seelesession.Session, error) {
	return nil, errors.New("not implemented")
}
func (fakeEnginePort) NewSubagentSessionWithID(string, *seelesession.LoopHooks) (*seelesession.Session, error) {
	return nil, errors.New("not implemented")
}
func (fakeEnginePort) ChatStreamFor(string, context.Context, string, func(string)) (string, error) {
	return "", errors.New("not implemented")
}
func (fakeEnginePort) UnloadSession(string) error                             { return nil }
func (fakeEnginePort) PrepareMainSessionHistory(string, []types.Message) bool { return false }

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
	if got := unit.Status(); got != StatusDraft {
		t.Fatalf("initial status = %q, want draft", got)
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

// TestLifecycleThinStateMachine（T2.3）：status 由 HasSession 驱动，
// 非法迁移拒绝（空 id、运行中 unload、未加载提交）。
func TestLifecycleThinStateMachine(t *testing.T) {
	unit, err := NewSessionUnit("sess-a")
	if err != nil {
		t.Fatalf("NewSessionUnit: %v", err)
	}

	// 未加载提交拒绝
	if err := unit.Submit(false); err == nil {
		t.Fatal("submit without loaded engine must be rejected")
	}
	// 未加载卸载幂等（不报错）
	if err := unit.Unload(); err != nil {
		t.Fatalf("unload of cold session: %v", err)
	}

	// 冷加载（HasSession=true）→ idle
	if err := unit.ColdLoad(true); err != nil {
		t.Fatalf("cold load: %v", err)
	}
	if got := unit.Status(); got != StatusIdle {
		t.Fatalf("after cold load status = %q, want idle", got)
	}
	if !unit.HasSession() {
		t.Fatal("HasSession must be true after cold load")
	}

	// 双 cold load 拒绝
	if err := unit.ColdLoad(true); err == nil {
		t.Fatal("double cold load must be rejected")
	}

	// idle --submit--> running
	if err := unit.Submit(true); err != nil {
		t.Fatalf("submit idle: %v", err)
	}
	if got := unit.Status(); got != StatusRunning {
		t.Fatalf("after submit status = %q, want running", got)
	}
	// running --submit--> queued（入队）
	if err := unit.Submit(true); err != nil {
		t.Fatalf("submit while running: %v", err)
	}
	if got := unit.Status(); got != StatusQueued {
		t.Fatalf("after running submit status = %q, want queued", got)
	}
	// 运行中 unload 拒绝
	if err := unit.Unload(); err == nil {
		t.Fatal("unload while running must be rejected")
	}
	// running --完成--> idle
	if err := unit.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if got := unit.Status(); got != StatusIdle {
		t.Fatalf("after finish status = %q, want idle", got)
	}
	// idle --unload--> draft（释放引擎）
	if err := unit.Unload(); err != nil {
		t.Fatalf("unload idle: %v", err)
	}
	if unit.HasSession() {
		t.Fatal("HasSession must be false after unload")
	}
	// cold 重建：cold --NewSessionWithID+seed--> idle
	if err := unit.ColdLoad(true); err != nil {
		t.Fatalf("re-cold-load: %v", err)
	}
	if got := unit.Status(); got != StatusIdle {
		t.Fatalf("after re-cold-load status = %q, want idle", got)
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

// TestIllegalLifecycleTransitions（B2）：空 id / 运行中 unload /
// 未加载提交 / 双 cold load 均拒绝。
func TestIllegalLifecycleTransitions(t *testing.T) {
	unit, err := NewSessionUnit("sess-b")
	if err != nil {
		t.Fatalf("NewSessionUnit: %v", err)
	}
	if err := unit.ColdLoad(true); err != nil {
		t.Fatalf("cold load: %v", err)
	}
	if err := unit.Submit(true); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := unit.Unload(); err == nil {
		t.Fatal("unload while running must be rejected")
	}
	if err := unit.ColdLoad(true); err == nil {
		t.Fatal("double cold load must be rejected")
	}
	// 未加载提交
	cold, _ := NewSessionUnit("sess-c")
	if err := cold.Submit(false); err == nil {
		t.Fatal("submit on cold session must be rejected")
	}
	if err := cold.Submit(true); err == nil {
		t.Fatal("submit before cold load must be rejected")
	}
	// 空 id
	if _, err := NewSessionUnit(" "); err == nil {
		t.Fatal("blank id must be rejected")
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
		if err := unit.ColdLoad(true); err != nil {
			t.Fatalf("cold load: %v", err)
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
					_ = unit.Submit(true)
					_ = unit.Finish()
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
