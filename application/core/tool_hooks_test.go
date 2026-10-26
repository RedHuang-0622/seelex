package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	subagent_view "github.com/RedHuang-0622/seelex/middle/view/subagent"
)

// TestHandleSubagentToolEventProjectsBoundedIncrementals 钉住子代理工具事件的
// 有界投影：详情面只保留上限内的参数/结果，不把整段长文本搬进快照。
func TestHandleSubagentToolEventProjectsBoundedIncrementals(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer frontendOfTestService(service).Shutdown()
	// 历史 Plan 投影（执行引擎已退役）：直接种一个含 worker 节点的只读状态。
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.Plan = &model.PlanState{
		Status: PlanRunning,
		Nodes:  []model.PlanNode{{ID: "worker", Status: NodeRunning}},
	}
	service.ViewMu.Unlock()

	// 事件流现包含 worktable.changed（CSP 汇聚，异步到达）：订阅缓冲调大，
	// 避免杂散生命周期事件触发 deliver 排空丢弃本测试关注的工具事件。
	subscription := frontendOfTestService(service).Subscribe(16)
	defer subscription.Close()
	long := strings.Repeat("x", Limits().EvidenceChars+20)
	started := seelsession.SubagentToolEvent{
		ID: "subtool-1", NodeID: "worker", Name: "read_file", Arguments: long,
		Status: dto.ToolEventRunning, StartedAt: time.Now(),
	}
	service.components.subagent.HandleSubagentToolEvent(started)
	completed := started
	completed.Status = dto.ToolEventSuccess
	completed.Result = long
	completed.Duration = time.Second
	service.components.subagent.HandleSubagentToolEvent(completed)

	snapshot := service.Snapshot()
	node := subagent_view.FindPlanNodeByID(snapshot.Runtime.Plan.Nodes, "worker")
	if node == nil || len(node.ToolEvents) != 1 {
		t.Fatalf("worker tool events = %#v", node)
	}
	if node.ToolEvents[0].Status != dto.ToolEventSuccess || len(node.ToolEvents[0].Arguments) > Limits().EvidenceChars+3 || len(node.ToolEvents[0].Result) > Limits().EvidenceChars+3 {
		t.Fatalf("tool event projection is not bounded: %+v", node.ToolEvents[0])
	}
}

func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer frontendOfTestService(service).Shutdown()
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	info := session.ToolCallInfo{Turn: 1, Name: "read", Arguments: `{"path":"a"}`}

	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)

	ids := make([]string, 0, 2)
	for _, message := range service.Snapshot().Conversation {
		if message.Role == "tool" && message.Tool != nil {
			ids = append(ids, message.Tool.ID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("tool IDs = %v, want two unique IDs", ids)
	}
}
