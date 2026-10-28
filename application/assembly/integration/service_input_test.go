package integration

import (
	"context"
	appevent "github.com/RedHuang-0622/seelex/application/event"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	limitapproval "github.com/RedHuang-0622/seelex/backend/limit/approval"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context"
	"strings"
	"testing"
	"time"
)

func TestLifecycleApplicationWaitForIdle(t *testing.T) {
	idle := make(chan struct{})
	close(idle)
	application := applicationlifecycle.New(applicationlifecycle.Deps{
		Idle: func() <-chan struct{} { return idle },
	})
	if err := application.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
}

func TestLifecycleApplicationShutdownRunsCleanupOnce(t *testing.T) {
	var calls []string
	closed := false
	application := applicationlifecycle.New(applicationlifecycle.Deps{
		BeginShutdown: func() bool {
			if closed {
				return false
			}
			closed = true
			calls = append(calls, "begin")
			return true
		},
		FlushToolRecords:       func() { calls = append(calls, "flush") },
		StopCatalogRefresh:     func() { calls = append(calls, "catalog") },
		CloseSessions:          func() { calls = append(calls, "sessions") },
		StopLifecycleConsumers: func() { calls = append(calls, "consumers") },
		CloseWorkTable:         func() { calls = append(calls, "worktable") },
		ShutdownApproval:       func() { calls = append(calls, "approval") },
	})
	application.Shutdown()
	application.Shutdown()
	want := "begin,flush,catalog,sessions,consumers,worktable,approval"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

// trustedSkillInHistory 断言引擎历史中存在激活技能 internal 事件
// （ActiveSkillMarker 标记、role=user）：技能正文在激活时 append 进 transcript，
// 装配随定稿轮次出现在引擎历史中（本任务真实轮次之前）——不再进 system。
func trustedSkillInHistory(t *testing.T, history []contract.EngineMessage, name, text string) bool {
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
	service := newLegacyTestService(t, engine)
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = engine.SessionID()
	service.components.tasks.SetPlanStateLocked(nil, "plan-x")
	service.Core.Snapshot.Runtime.Plan = &model.PlanState{
		Status: model.PlanRunning,
		Nodes:  []model.PlanNode{{ID: "n1", Label: "第一步", Status: model.NodeRunning}},
	}
	service.components.tasks.BeginTask("chat-x", "", "high", nil, model.TaskCheckpoint{})
	first := service.components.prompts.SystemPromptForActiveTaskLocked()
	service.Core.Snapshot.Runtime.Plan.Nodes[0].Status = model.NodeCompleted
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
	service := newLegacyTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	block := service.components.workTableRuntime.TraceBlock()
	if !strings.HasPrefix(block, applicationtable.TraceMarkerOpen) || !strings.HasSuffix(block, applicationtable.TraceMarkerClose) {
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
	if got := service.components.workTableRuntime.TraceBlock(); got != "" {
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
	service := newLegacyTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	out, err := service.components.context.PrepareExecutionContext("task-1", "继续")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, applicationtable.TraceMarkerOpen) || !strings.Contains(out, "继续") {
		t.Fatalf("request input must carry the trace block before the input: %q", out)
	}

	runtime.tasks = map[string]dto.TaskRecord{}
	out, err = service.components.context.PrepareExecutionContext("task-1", "继续")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, applicationtable.TraceMarkerOpen) {
		t.Fatalf("trace block must be removed when no active tasks: %q", out)
	}
}

// TestBeginNewSessionKeepsGlobalWorkTable 见 service_test.go：工作表格是
// 项目/全局台账，/new 不清空跨会话表。

func TestSuggestionsAndSkillRouting(t *testing.T) {
	engine := &fakeEngine{}
	service := newLegacyTestService(t, engine)
	defer service.components.lifecycleApplication.Shutdown()
	suggestions := service.components.completionApplication.Suggestions("/R")
	if len(suggestions) != 2 || suggestions[0].Kind != "command" || suggestions[1].Kind != "skill" {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
	// sigil 契约：每个前缀只回自己那一域（`/` 可执行入口 + `#` Plugin + `$` Skill +
	// `@` 团队），不互相兜底。`/` 列命令与 Skill 两类可执行入口（工具不在这里——
	// 要用 /名字 显式调用先把它注册成命令），因此只对它断言"含有该域候选"，其余前缀
	// 断言"整列只有一个域"。
	//
	// `@` 的候选来自**团队库**（不是零 I/O 的内存注册表）：Suggestions 每次读到的是
	// 进程内快照，库在写路径/读回时让快照过期（见 completion.go 末尾）。本夹具的会话
	// 端口没有团队库，所以 `@` 不进下面这张"必须非空"的表——它的建议面由
	// team_summon_suggestions_repro_test.go 钉。
	for _, testCase := range []struct {
		input, kind, member string
		exclusive           bool
	}{
		{"/", applicationinput.SuggestionKindCommand, "help", false},
		{"#", applicationinput.SuggestionKindPlugin, "code", true},
		{"$", applicationinput.SuggestionKindSkill, "review", true},
	} {
		got := service.components.completionApplication.Suggestions(testCase.input)
		if len(got) == 0 {
			t.Fatalf("%q 建议为空", testCase.input)
		}
		names := make([]string, 0, len(got))
		memberKind := ""
		for _, suggestion := range got {
			names = append(names, suggestion.Text)
			if suggestion.Text == testCase.member {
				memberKind = suggestion.Kind
			}
			if testCase.exclusive && suggestion.Kind != testCase.kind {
				t.Fatalf("%q 建议 %q 的 kind = %q, want %q", testCase.input, suggestion.Text, suggestion.Kind, testCase.kind)
			}
		}
		if memberKind != testCase.kind {
			t.Fatalf("%q 建议里 %q 的 kind = %q, want %q（%v）", testCase.input, testCase.member, memberKind, testCase.kind, names)
		}
	}
	// 未知前缀与进入参数区（含空格）都不给建议（不弹面板）。
	if got := service.components.completionApplication.Suggestions("%review"); got != nil {
		t.Fatalf("未知前缀不该给建议：%#v", got)
	}
	// `@` 的候选在**团队库**里，而本夹具的会话端口（fakeSessions）没有团队库读写面：
	// 取不到库时不该炸、也不该弹一个空面板。真正的 `@` 建议面（列出库里的团队、
	// 前缀过滤、写库/删库即时反映）由 team_summon_suggestions_repro_test.go 钉。
	if got := service.components.completionApplication.Suggestions("@"); len(got) != 0 {
		t.Fatalf("宿主未装配团队库时 `@` 不该有候选：%#v", got)
	}
	if got := service.components.completionApplication.Suggestions("#code prompt"); got != nil {
		t.Fatalf("参数区不该给建议：%#v", got)
	}
	if err := service.components.submission.Submit(context.Background(), "/review strict"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt := engine.prompt
	modelInput := engine.lastInput
	firstSentHistory := append([]contract.EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	// system 只含稳定 base + 被动技能目录；激活技能正文已移出 system，
	// 改为激活时 append-only 落进 transcript（internal user 事件）。
	if strings.Contains(prompt, "## Trusted Active Skill") || strings.Contains(prompt, "review prompt") {
		t.Fatalf("skill body must not be embedded in system prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "Seelex") {
		t.Fatalf("prompt missing identity: %q", prompt)
	}
	if !strings.Contains(prompt, "## Effort Workflow") {
		t.Fatalf("prompt missing effort: %q", prompt)
	}
	if modelInput != "/review strict" {
		t.Fatalf("slash Skill model input = %q", modelInput)
	}
	if !trustedSkillInHistory(t, firstSentHistory, "review", "review prompt") {
		t.Fatalf("activated skill body must appear in assembled engine history: %#v", firstSentHistory)
	}
	// `$` 是 Skill 的专用前缀（`#` 改成"切换插件"后由它接管）。
	if err := service.components.submission.Submit(context.Background(), "$review focused"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt = engine.prompt
	modelInput = engine.lastInput
	secondSentHistory := append([]contract.EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if modelInput != "$review focused" || strings.Contains(prompt, "## Trusted Active Skill") {
		t.Fatalf("dollar Skill input=%q prompt=%q", modelInput, prompt)
	}
	if !trustedSkillInHistory(t, secondSentHistory, "review", "review prompt") {
		t.Fatalf("dollar skill body must appear in assembled engine history: %#v", secondSentHistory)
	}
	// 被动技能目录：随插件装配自动注入（不依赖模型调用 skills_list）。
	if !strings.Contains(prompt, "## Available Skills") || !strings.Contains(prompt, "- review: review code") {
		t.Fatalf("prompt missing passive skill catalog: %q", prompt)
	}
	// 激活纪律句里的用户入口前缀必须取自 sigil 表（SigilSkill）：`#` 自
	// 2026-09-17 起是"切换插件"，提示里若再写 `#<name>` 就是把模型教错、
	// 再由模型把用户教错（历史事故：该句写死前缀，前缀改了它没改）。
	if !strings.Contains(prompt, "send "+applicationinput.SigilSkill+"<name>") || strings.Contains(prompt, "#<name>") {
		t.Fatalf("skill catalog activation hint must use %s<name>: %q", applicationinput.SigilSkill, prompt)
	}
}

func TestApprovalBrokerResolve(t *testing.T) {
	hub := appevent.NewEventHub()
	subscription := hub.Subscribe(1)
	defer subscription.Close()
	broker := limitapproval.NewApprovalBroker(hub)
	result := make(chan contract.ApprovalDecision, 1)
	go func() {
		decision, err := broker.Request(context.Background(), contract.ApprovalRequest{ID: "approval-1", Question: "continue?", Options: []model.InteractionOption{{ID: "yes", Label: "Yes"}}})
		if err == nil {
			result <- decision
		}
	}()
	select {
	case event := <-subscription.Events:
		if event.Kind != appevent.EventInteractionOpened {
			t.Fatalf("opened event kind = %q, want %q", event.Kind, appevent.EventInteractionOpened)
		}
	case <-time.After(time.Second):
		t.Fatal("approval request was not opened")
	}
	if err := broker.Resolve("approval-1", contract.ApprovalDecision{OptionID: "yes"}); err != nil {
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
