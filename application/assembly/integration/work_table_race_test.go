package integration

import (
	"fmt"
	. "github.com/RedHuang-0622/seelex/application/assembly"
	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"sync"
	"testing"
	"time"
)

// TestWorkTableRaceConcurrentMutations 并发执行工作表格三类变更路径：
// UpdateWorkItemStatus（todo actor mailbox）、HandleSubagentToolEvent（plan
// 节点 trace）、Snapshot（读路径）。配合 -race 验证 service.ViewMu 与 actor
// mailbox 的并发安全（CI 运行 -race -covermode=atomic）。

func TestWorkTableRaceConcurrentMutations(t *testing.T) {
	runtime := &fakeRuntime{todoItems: []dto.TodoItem{
		{Text: "a", Status: dto.TodoItemPending},
		{Text: "b", Status: dto.TodoItemPending},
		{Text: "c", Status: dto.TodoItemPending},
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.Plan = &model.PlanState{
		Status: model.PlanRunning,
		Nodes:  []model.PlanNode{{ID: "n1", Label: "并行任务", Status: model.NodeRunning}},
	}
	ComponentsFor(service).Execution.WorkTable.RefreshLocked(service.Deps.Runtime.TaskSnapshot(), ComponentsFor(service).Execution.WorkTable.AsyncRuns())
	service.ViewMu.Unlock()

	const workers = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_ = ComponentsFor(service).Execution.WorkItem.UpdateWorkItemStatus(fmt.Sprintf("todo:%d", index%3), "doing")
		}(index)
	}
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			ComponentsFor(service).Execution.SubagentView.HandleSubagentToolEvent(seelsession.SubagentToolEvent{
				ID: fmt.Sprintf("tool-%d", index), NodeID: "n1", Name: "read_file",
				Status: dto.ToolEventSuccess, StartedAt: time.Now(),
			})
		}(index)
	}
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = ComponentsFor(service).Session.Snapshot.Snapshot()
		}()
	}
	close(start)
	wg.Wait()

	snapshot := ComponentsFor(service).Session.Snapshot.Snapshot()
	table := snapshot.Runtime.WorkTable
	if len(table) < 3 { // 3 todo（注册表为唯一 task 源；plan 节点需经被动同步入表）
		t.Fatalf("work table rows = %d, want >= 3", len(table))
	}
}
