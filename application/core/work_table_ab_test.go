package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── A/B 对比：worktable.changed（B）vs 整份 runtime.changed（A）──
// 断言：同状态下工作表格增量 payload 体积 ≤ 整份 runtime 的 30%。
// 完整方法与基线记录在 docs/test/worktable-ab.md。

func TestWorkTablePayloadSmallerThanFullRuntime(t *testing.T) {
	plan := heavyTestPlan(20)
	tasks := make([]dto.TaskRecord, 20)
	for index := range tasks {
		tasks[index] = dto.TaskRecord{
			ID: "todo:" + string(rune('a'+index)), Phase: "tasklist", Task: "todo item",
			Status: dto.TaskDoing, Kind: "todo",
		}
	}
	tree := heavySubagentTree(10)

	table := buildWorkTable(plan, tasks, tree)
	if len(table) > Limits().WorkTableRows {
		t.Fatalf("table rows = %d exceed limit %d", len(table), Limits().WorkTableRows)
	}

	workPayload, err := json.Marshal(WorkTableEvent{Items: table})
	if err != nil {
		t.Fatal(err)
	}
	runtime := RuntimeState{Plan: plan, WorkTable: table}
	fullPayload, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	ratio := float64(len(workPayload)) / float64(len(fullPayload))
	if ratio > 0.30 {
		t.Fatalf("worktable payload ratio = %.2f (work=%d bytes, full=%d bytes), want <= 0.30",
			ratio, len(workPayload), len(fullPayload))
	}
	t.Logf("worktable.changed=%d bytes vs runtime.changed=%d bytes (ratio=%.2f)",
		len(workPayload), len(fullPayload), ratio)
}

func heavyTestPlan(nodes int) *PlanState {
	plan := &PlanState{Status: PlanRunning}
	for index := 0; index < nodes; index++ {
		id := string(rune('a' + index))
		events := make([]PlanNodeEventInfo, 0, 30)
		tools := make([]SubagentToolEvent, 0, 30)
		for eventIndex := 0; eventIndex < 30; eventIndex++ {
			events = append(events, PlanNodeEventInfo{
				Status: NodeStatus("running"), At: time.Now(), Output: "节点事件证据文本，用于模拟长任务打点",
			})
			tools = append(tools, SubagentToolEvent{
				ID: "tool", NodeID: id, Name: "read_file", Arguments: "证据参数", Result: "证据结果",
				Status: "success", StartedAt: time.Now(), Duration: 1234 * time.Millisecond,
			})
		}
		plan.Nodes = append(plan.Nodes, PlanNode{
			ID: id, Label: "任务节点", Kind: "auto", Status: NodeRunning,
			Output: "节点输出证据文本", Events: events, ToolEvents: tools,
		})
	}
	return plan
}

func heavySubagentTree(rows int) []dto.SubAgentTreeNode {
	nodes := make([]dto.SubAgentTreeNode, 0, rows)
	for index := 0; index < rows; index++ {
		nodes = append(nodes, dto.SubAgentTreeNode{
			ID: "sub", Goal: "子代理目标", Status: dto.SubAgentRunning, Summary: "子代理摘要",
			StartedAt: time.Now(),
			Context: &dto.SubAgentNodeContext{
				Goal: "子代理目标", Progress: "进行中", MessageCount: 12, TokenEstimate: 3456,
				Findings: []string{"发现一", "发现二", "发现三"},
			},
		})
	}
	return nodes
}
