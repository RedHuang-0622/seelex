package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestSystemPromptStableAcrossPlanNodeChanges(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.Mu.Lock()
	service.components.tasks.SetPlanStateLocked(nil, "plan-x")
	service.Core.Snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes:  []PlanNode{{ID: "n1", Label: "第一步", Status: NodeRunning}},
	}
	service.components.tasks.BeginTask("chat-x", "", "high", nil, TaskCheckpoint{})
	first := service.components.prompts.SystemPromptForActiveTaskLocked()
	service.Core.Snapshot.Runtime.Plan.Nodes[0].Status = NodeCompleted
	second := service.components.prompts.SystemPromptForActiveTaskLocked()
	service.Mu.Unlock()
	if first != second {
		t.Fatalf("system prompt must be byte-stable across node transitions:\nfirst=%q\nsecond=%q", first, second)
	}
	if strings.Contains(first, "current_node=") {
		t.Fatal("system prompt must not embed current_node")
	}
	if !strings.Contains(first, "plan_ref=plan-x") {
		t.Fatalf("system prompt should keep the stable plan_ref: %q", first)
	}
}

// TestWorkTableTraceBlock 打点表标记块：只含未终态任务（终态即删）、
// 带标记围栏、retry 计数、无活动任务返回空。

func TestWorkTableTraceBlock(t *testing.T) {
	runtime := &fakeRuntime{}
	runtime.tasks = map[string]dto.TaskRecord{
		"task:1": {ID: "task:1", Phase: "task", Task: "分析模块", Status: dto.TaskRunning, RetryCount: 2},
		"todo:0": {ID: "todo:0", Phase: "tasklist", Task: "写测试", Status: dto.TaskDoing},
		"plan:x": {ID: "plan:x", Phase: "plan", Task: "已结束", Status: dto.TaskCompleted},
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	block := service.workTableTraceBlock()
	if !strings.HasPrefix(block, workTableTraceMarkerOpen) || !strings.HasSuffix(block, workTableTraceMarkerClose) {
		t.Fatalf("trace block must be wrapped in markers: %q", block)
	}
	if !strings.Contains(block, "task:1 running retry=2") || !strings.Contains(block, "todo:0 doing") {
		t.Fatalf("trace block must carry active tasks: %q", block)
	}
	if strings.Contains(block, "plan:x") {
		t.Fatalf("terminal task must be deleted from the trace block: %q", block)
	}

	// 全部终态 → 块为空（打点完即删除）。
	runtime.tasks = map[string]dto.TaskRecord{
		"plan:x": {ID: "plan:x", Phase: "plan", Task: "已结束", Status: dto.TaskCompleted},
	}
	if got := service.workTableTraceBlock(); got != "" {
		t.Fatalf("trace block must be empty when no active tasks: %q", got)
	}
}

// TestPrepareExecutionContextCarriesWorkTableTraceBlock 请求尾部注入打点表：
// 有活动任务时输入带标记块；无活动任务时不带（块随任务完成删除）。

func TestPrepareExecutionContextCarriesWorkTableTraceBlock(t *testing.T) {
	runtime := &fakeRuntime{}
	runtime.tasks = map[string]dto.TaskRecord{
		"task:1": {ID: "task:1", Phase: "task", Task: "分析模块", Status: dto.TaskRunning},
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	out, err := service.components.context.PrepareExecutionContext("task-1", "继续")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, workTableTraceMarkerOpen) || !strings.Contains(out, "继续") {
		t.Fatalf("request input must carry the trace block before the input: %q", out)
	}

	runtime.tasks = map[string]dto.TaskRecord{}
	out, err = service.components.context.PrepareExecutionContext("task-1", "继续")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, workTableTraceMarkerOpen) {
		t.Fatalf("trace block must be removed when no active tasks: %q", out)
	}
}

// TestBeginNewSessionClearsWorkTable 会话级工作台隔离：新建会话清空 task
// 注册表与工作表格，旧会话数据不污染新会话。

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(Dependencies{}); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestSuggestionsAndSkillRouting(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	suggestions := service.Suggestions("/R")
	if len(suggestions) != 3 || suggestions[0].Kind != "command" || suggestions[1].Kind != "tool" || suggestions[2].Kind != "skill" {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
	if err := service.Submit(context.Background(), "/review strict"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt := engine.prompt
	modelInput := engine.lastInput
	engine.mu.Unlock()
	if !strings.Contains(prompt, "## Trusted Active Skill: review") || !strings.Contains(prompt, "review prompt") || strings.Contains(prompt, "strict") {
		t.Fatalf("trusted Skill system prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "Seelex") {
		t.Fatalf("prompt missing identity: %q", prompt)
	}
	if !strings.Contains(prompt, "## Effort: High") {
		t.Fatalf("prompt missing effort: %q", prompt)
	}
	if modelInput != "/review strict" {
		t.Fatalf("slash Skill model input = %q", modelInput)
	}
	if err := service.Submit(context.Background(), "#review focused"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt = engine.prompt
	modelInput = engine.lastInput
	engine.mu.Unlock()
	if modelInput != "#review focused" || !strings.Contains(prompt, "## Trusted Active Skill: review") || !strings.Contains(prompt, "review prompt") {
		t.Fatalf("hash Skill input=%q prompt=%q", modelInput, prompt)
	}
}

func TestApprovalBrokerResolve(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.Subscribe(1)
	defer subscription.Close()
	broker := NewApprovalBroker(hub)
	result := make(chan ApprovalDecision, 1)
	go func() {
		decision, err := broker.Request(context.Background(), ApprovalRequest{ID: "approval-1", Question: "continue?", Options: []InteractionOption{{ID: "yes", Label: "Yes"}}})
		if err == nil {
			result <- decision
		}
	}()
	select {
	case event := <-subscription.Events:
		if event.Kind != EventInteractionOpened {
			t.Fatalf("opened event kind = %q, want %q", event.Kind, EventInteractionOpened)
		}
	case <-time.After(time.Second):
		t.Fatal("approval request was not opened")
	}
	if err := broker.Resolve("approval-1", ApprovalDecision{OptionID: "yes"}); err != nil {
		t.Fatal(err)
	}
	select {
	case decision := <-result:
		if decision.OptionID != "yes" {
			t.Fatalf("unexpected decision %#v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not resolve")
	}
}
