package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// trustedSkillInHistory 断言引擎历史中存在激活技能 internal 事件
// （ActiveSkillMarker 标记、role=user）：技能正文在激活时 append 进 transcript，
// 装配随定稿轮次出现在引擎历史中（本任务真实轮次之前）——不再进 system。
func trustedSkillInHistory(t *testing.T, history []EngineMessage, name, text string) bool {
	t.Helper()
	for _, message := range history {
		if message.Role == "user" && strings.HasPrefix(message.Content, context_runtime.ActiveSkillPrefix) {
			return strings.Contains(message.Content, "## Trusted Active Skill: "+name) && strings.Contains(message.Content, text)
		}
	}
	return false
}

func TestSystemPromptStableAcrossPlanNodeChanges(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = engine.SessionID()
	service.components.tasks.SetPlanStateLocked(nil, "plan-x")
	service.Core.Snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes:  []PlanNode{{ID: "n1", Label: "第一步", Status: NodeRunning}},
	}
	service.components.tasks.BeginTask("chat-x", "", "high", nil, TaskCheckpoint{})
	first := service.components.prompts.SystemPromptForActiveTaskLocked()
	service.Core.Snapshot.Runtime.Plan.Nodes[0].Status = NodeCompleted
	second := service.components.prompts.SystemPromptForActiveTaskLocked()
	service.ViewMu.Unlock()
	if first != second {
		t.Fatalf("system prompt must be byte-stable across node transitions:\nfirst=%q\nsecond=%q", first, second)
	}
	if strings.Contains(first, "current_node=") {
		t.Fatal("system prompt must not embed current_node")
	}
	// plan 执行指令与 plan_ref 已移出 system：不再出现随 plan 加载/完成而改写
	// 头部的动态段（system 成为跨 plan 稳定常量）。
	if strings.Contains(first, "## Active Plan Execution Policy") || strings.Contains(first, "plan_ref=") {
		t.Fatalf("plan execution policy must not live in system: %q", first)
	}
	// plan 执行指令落在请求尾部 plan 上下文消息：Prepare 后引擎历史末条 user
	// 消息同时携带策略与 plan_ref（节点状态每轮刷新，天然在未命中后缀区）。
	if _, err := service.components.context.PrepareExecutionContext("chat-x", "next"); err != nil {
		t.Fatal(err)
	}
	var tail string
	for _, message := range engine.History() {
		if strings.Contains(message.Content, "plan_ref=plan-x") || strings.Contains(message.Content, "## Active Plan Execution Policy") {
			tail = message.Content
			if message.Role != "system" {
				t.Fatalf("plan tail/state material must use provider system, got %q: %q", message.Role, message.Content)
			}
		}
	}
	if !strings.Contains(tail, "plan_ref=plan-x") || !strings.Contains(tail, "## Active Plan Execution Policy") {
		t.Fatalf("plan tail message must carry policy + plan_ref: %q", tail)
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
	firstSentHistory := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	// system 只含稳定 base + 被动技能目录；激活技能正文已移出 system，
	// 改为激活时 append-only 落进 transcript（internal user 事件）。
	if strings.Contains(prompt, "## Trusted Active Skill") || strings.Contains(prompt, "review prompt") {
		t.Fatalf("skill body must not be embedded in system prompt: %q", prompt)
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
	if !trustedSkillInHistory(t, firstSentHistory, "review", "review prompt") {
		t.Fatalf("activated skill body must appear in assembled engine history: %#v", firstSentHistory)
	}
	if err := service.Submit(context.Background(), "#review focused"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt = engine.prompt
	modelInput = engine.lastInput
	secondSentHistory := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if modelInput != "#review focused" || strings.Contains(prompt, "## Trusted Active Skill") {
		t.Fatalf("hash Skill input=%q prompt=%q", modelInput, prompt)
	}
	if !trustedSkillInHistory(t, secondSentHistory, "review", "review prompt") {
		t.Fatalf("hash skill body must appear in assembled engine history: %#v", secondSentHistory)
	}
	// 被动技能目录：随插件装配自动注入（不依赖模型调用 skills_list）。
	if !strings.Contains(prompt, "## Available Skills") || !strings.Contains(prompt, "- review: review code") {
		t.Fatalf("prompt missing passive skill catalog: %q", prompt)
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
