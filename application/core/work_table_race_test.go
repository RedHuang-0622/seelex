package core

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// TestWorkTableRaceConcurrentMutations 并发执行工作表格三类变更路径：
// UpdateWorkItemStatus（todo actor mailbox）、HandleSubagentToolEvent（plan
// 节点 trace）、Snapshot（读路径）。配合 -race 验证 service.mu 与 actor
// mailbox 的并发安全（CI 运行 -race -covermode=atomic）。
func TestWorkTableRaceConcurrentMutations(t *testing.T) {
	runtime := &fakeRuntime{todoItems: []seelebridge.TodoItem{
		{Text: "a", Status: seelebridge.TodoItemPending},
		{Text: "b", Status: seelebridge.TodoItemPending},
		{Text: "c", Status: seelebridge.TodoItemPending},
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.mu.Lock()
	service.snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes:  []PlanNode{{ID: "n1", Label: "并行任务", Status: NodeRunning}},
	}
	service.refreshWorkTableLocked(service.deps.Runtime.TaskSnapshot())
	service.mu.Unlock()

	const workers = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_ = service.UpdateWorkItemStatus(fmt.Sprintf("todo:%d", index%3), "doing")
		}(index)
	}
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			service.HandleSubagentToolEvent(seelebridge.SubagentToolEvent{
				ID: fmt.Sprintf("tool-%d", index), NodeID: "n1", Name: "read_file",
				Status: "success", StartedAt: time.Now(),
			})
		}(index)
	}
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = service.Snapshot()
		}()
	}
	close(start)
	wg.Wait()

	snapshot := service.Snapshot()
	table := snapshot.Runtime.WorkTable
	if len(table) < 3 { // 3 todo（注册表为唯一 task 源；plan 节点需经被动同步入表）
		t.Fatalf("work table rows = %d, want >= 3", len(table))
	}
}
