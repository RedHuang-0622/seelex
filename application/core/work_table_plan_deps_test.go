package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// plan 行的工作表格「依赖」列：plan_load 产出的是**平铺节点表 + 边集**
// （Nodes + Edges），依赖只能取自 DAG 入边——按树形父子推导会让 plan 行的
// 依赖恒为空，用户在表格里看不到节点在等谁。

// TestWorkTablePlanRowsCarryDAGDependencies 投影面：平铺节点 + 边集 → 行依赖。
func TestWorkTablePlanRowsCarryDAGDependencies(t *testing.T) {
	plan := &PlanState{
		Status: PlanRunning,
		Nodes: []PlanNode{
			{ID: "inspect", Label: "inspect", Status: NodeCompleted},
			{ID: "implement", Label: "implement", Status: NodeRunning},
			{ID: "verify", Label: "verify", Status: NodePending},
		},
		Edges: []dto.PlanEdge{
			{From: "inspect", To: "implement"},
			{From: "implement", To: "verify"},
			// 多入边 + 重复边：去重且稳定排序。
			{From: "inspect", To: "verify"},
			{From: "inspect", To: "verify"},
		},
	}
	tasks := []dto.TaskRecord{
		{ID: "plan:inspect", Key: "plan:inspect", Phase: "plan", Task: "inspect", Kind: "plan", SourceID: "inspect"},
		{ID: "plan:implement", Key: "plan:implement", Phase: "plan", Task: "implement", Kind: "plan", SourceID: "implement"},
		{ID: "plan:verify", Key: "plan:verify", Phase: "plan", Task: "verify", Kind: "plan", SourceID: "verify"},
	}
	rows := buildWorkTable(plan, tasks, nil, nil)
	byID := map[string]WorkItem{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if deps := byID["plan:inspect"].Dependencies; len(deps) != 0 {
		t.Fatalf("入口节点依赖 = %v, want 空（没有前置）", deps)
	}
	if deps := byID["plan:implement"].Dependencies; len(deps) != 1 || deps[0] != "plan:inspect" {
		t.Fatalf("implement 依赖 = %v, want [plan:inspect]", deps)
	}
	verify := byID["plan:verify"].Dependencies
	if len(verify) != 2 || verify[0] != "plan:implement" || verify[1] != "plan:inspect" {
		t.Fatalf("verify 依赖 = %v, want [plan:implement plan:inspect]（去重 + 排序）", verify)
	}
}

// TestServicePlanSyncExposesDAGDependencies 走 Service 路径：plan 投影 → task
// 同步 → 工作表格行带出依赖（用户看到的「依赖」列不再恒为空）。
func TestServicePlanSyncExposesDAGDependencies(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.Core.Snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes: []PlanNode{
			{ID: "n1", Label: "inspect", Status: NodeCompleted},
			{ID: "n2", Label: "implement", Status: NodeRunning},
			{ID: "n3", Label: "verify", Status: NodePending},
		},
		Edges: []dto.PlanEdge{{From: "n1", To: "n2"}, {From: "n2", To: "n3"}},
	}
	service.refreshWorkTableFromSources()

	byID := map[string]WorkItem{}
	for _, row := range service.Snapshot().Runtime.WorkTable {
		byID[row.ID] = row
	}
	if len(byID) != 3 {
		t.Fatalf("工作表格行数 = %d, want 3（%v）", len(byID), byID)
	}
	if deps := byID["plan:n2"].Dependencies; len(deps) != 1 || deps[0] != "plan:n1" {
		t.Fatalf("plan:n2 依赖 = %v, want [plan:n1]", deps)
	}
	if deps := byID["plan:n3"].Dependencies; len(deps) != 1 || deps[0] != "plan:n2" {
		t.Fatalf("plan:n3 依赖 = %v, want [plan:n2]", deps)
	}
}

// TestTaskChangedIncrementCarriesPlanDependencies 增量面：task.changed 单行
// 必须与整表同源补齐依赖，否则前端按增量更新该行时会把「依赖」列擦成空。
func TestTaskChangedIncrementCarriesPlanDependencies(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionID := service.Snapshot().Session.ID
	service.Core.Snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes: []PlanNode{
			{ID: "n1", Label: "inspect", Status: NodeCompleted},
			{ID: "n2", Label: "implement", Status: NodeRunning},
		},
		Edges: []dto.PlanEdge{{From: "n1", To: "n2"}},
	}
	subscription := service.Subscribe(16)
	defer subscription.Close()

	service.publishTaskChanged(dto.TaskRecord{
		ID: "plan:n2", Key: "plan:n2", Phase: dto.TaskPhasePlan, Task: "implement",
		Status: dto.TaskRunning, Kind: "plan", SourceID: "n2",
	}, 1, "req-plan", sessionID)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-subscription.Events:
			if event.Kind != EventTaskChanged {
				continue
			}
			var payload TaskChangedEvent
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode task.changed: %v", err)
			}
			if payload.TaskID != "plan:n2" {
				t.Fatalf("task.changed task_id = %q, want plan:n2", payload.TaskID)
			}
			deps := payload.Task.Dependencies
			if len(deps) != 1 || deps[0] != "plan:n1" {
				t.Fatalf("task.changed 依赖 = %v, want [plan:n1]", deps)
			}
			return
		case <-deadline:
			t.Fatal("timeout waiting for task.changed")
		}
	}
}
