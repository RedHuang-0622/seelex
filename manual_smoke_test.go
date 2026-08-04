//go:build manualsmoke

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	seelexctxsnapshot "github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// TestManualSmokeRealAccountPlan verifies the real account path without
// exposing its contents. It is intentionally opt-in because it makes a paid
// network request. Run it with:
//
//	$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
//	go test -tags manualsmoke . -run TestManualSmokeRealAccountPlan -count=1 -timeout=5m
func TestManualSmokeRealAccountPlan(t *testing.T) {
	accountsPath := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsPath == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}

	tempDir := t.TempDir()
	tempAccounts := filepath.Join(tempDir, "accounts.yaml")
	copyOpaqueFile(t, accountsPath, tempAccounts)

	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath:    tempAccounts,
		StorePath:       filepath.Join(tempDir, "runtime"),
		ToolCallTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("initialize runtime from opaque temporary accounts file: %v", err)
	}
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	// 冒烟场景 = goal 流程（plan 全链路）：plan 工具面归位后（plan.md §6）
	// plan 工具仅在 goal skill 激活时对主代理可见，冒烟模拟 goal 场景。
	runtime.SetGoalSkillProvider(func() bool { return true })

	originalStorePath := *storePath
	*storePath = filepath.Join(tempDir, "sessions")
	defer func() { *storePath = originalStorePath }()

	skills := initSkillSystem()
	plugins, err := initPluginSystem(runtime, skills)
	if err != nil {
		t.Fatal(err)
	}
	hooks := application.NewToolHookBridge()
	frameworkEngine, err := initEngine(runtime, hooks, "")
	if err != nil {
		t.Fatal(err)
	}
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	runtime.SetPlanApprovalGate(&planApprovalGate{broker: approval})
	if err := activateDefaultPlugin(plugins, frameworkEngine); err != nil {
		t.Fatal(err)
	}

	appEngine := newEnginePort(frameworkEngine, func(sessionID string) reactorEngine {
		fresh, createErr := initEngine(runtime, hooks, sessionID)
		if createErr != nil {
			return nil
		}
		return fresh
	}, runtime.Tracer())
	store, err := initStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspaces, err := initWorkspaceRepo()
	if err != nil {
		t.Fatal(err)
	}
	app, err := initApplication(
		appEngine,
		runtime,
		plugins,
		initSessionManager(store, appEngine),
		skills,
		workspaces,
		events,
		approval,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	hooks.Bind(app)
	runtime.SetPlanNodeCallback(app.HandlePlanNodeComplete)
	// 终态/打点工具注册（main() 同款）：task_complete / task_failed /
	// task_needs_user_decision / task_check_node。此前冒烟漏掉此调用，
	// 模型正确报告"task_complete not available"——终态工具在冒烟 runtime
	// 中从未注册，计划类阶段的模型行为因此失真。
	registerTaskTerminalTools(runtime, app)
	// 子代理上下文闭环（与 main.go 同款，Actor 消息边界）：父证据注入走
	// application 镜像 + 遥测（绝不访问主会话——plan_run 期间主会话被
	// ChatStream 持锁，直接访问会死锁）；merge-back 经 mailbox 排队注入。
	runtime.SetContextExchanger(&smokeContextExchanger{
		app: app, tracer: runtime.Tracer(),
	})
	// 项目作用域：冒烟测试未走 GUI 的 workspace 选择流程，显式绑定当前
	// 目录——scoped 工具（glob/read_file/grep）依赖 project root，未绑定
	// 时全部失败，模型探测受挫后行为漂移（plan_run 阶段 glob 失败的根因）。
	if err := runtime.BindProjectRoot(workspaceSmokeRoot()); err != nil {
		t.Fatalf("bind smoke project root: %v", err)
	}

	mediumCtx, cancelMedium := smokePhaseContext()
	defer cancelMedium()
	if err := app.SwitchEffort(mediumCtx, "medium"); err != nil {
		t.Fatalf("switch to medium effort: %v", err)
	}
	if err := app.Submit(mediumCtx, "#plan"); err != nil {
		t.Fatalf("activate plan skill: %v", err)
	}
	mediumObserver := newSmokeObserver(events)
	if err := app.Submit(mediumCtx, "Use plan_load exactly once. Load a serial two-node plan with entry node inspect and a second node report; each node must have a short input. Do not execute the plan. Then call task_needs_user_decision with a short question that asks whether the user wants the loaded plan executed, and reply with PLAN_SMOKE_OK. Do not call any other tool."); err != nil {
		t.Fatalf("submit live plan request: %v", err)
	}
	if err := app.WaitForIdle(mediumCtx); err != nil {
		mediumObserver.Dump(t, "medium")
		t.Fatalf("wait for live plan request: %v", err)
	}
	mediumObserver.Dump(t, "medium")
	mediumObserver.Close()

	snapshot := app.Snapshot()
	if snapshot.Chat.Error != "" {
		t.Fatalf("live plan request failed: %s", snapshot.Chat.Error)
	}
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.EntryNodeID != "inspect" {
		t.Fatalf("live plan state = %#v, want loaded inspect plan", snapshot.Runtime.Plan)
	}
	loaded := false
	for _, message := range snapshot.Conversation {
		if message.Tool != nil && message.Tool.Name == "plan_load" && message.Tool.Status == "success" {
			loaded = true
			break
		}
	}
	if !loaded {
		for index := len(snapshot.Conversation) - 1; index >= 0; index-- {
			message := snapshot.Conversation[index]
			if message.Role == "assistant" && message.Content != "" {
				t.Fatalf("live request completed without a successful plan_load tool call; assistant reply: %q", truncateSmokeReply(message.Content, 500))
			}
		}
		t.Fatal("live request completed without a successful plan_load tool call")
	}
	mediumPlanLoads := 0
	for _, message := range snapshot.Conversation {
		if message.Role == "tool" && message.Tool != nil && message.Tool.Name == "plan_load" && message.Tool.Status == "success" {
			mediumPlanLoads++
		}
	}
	if mediumPlanLoads != 1 || len(snapshot.Runtime.Plan.Nodes) != 2 || len(snapshot.Runtime.Plan.Edges) != 1 {
		t.Fatalf("medium authority result: plan_loads=%d nodes=%d edges=%d, want one serial two-node Plan", mediumPlanLoads, len(snapshot.Runtime.Plan.Nodes), len(snapshot.Runtime.Plan.Edges))
	}
	t.Logf("authoritative medium preflight_plan_loads=%d serial_nodes=%d serial_edges=%d plan_run=0", mediumPlanLoads, len(snapshot.Runtime.Plan.Nodes), len(snapshot.Runtime.Plan.Edges))

	// High effort planning is voluntary: the model must obey the explicit
	// plan_load instruction through the normal ReAct path (强制规划 gate 已移除，
	// 规划永远是模型的自愿决策；此处验证 high effort 下 plan_load 工具仍可用)。
	highCtx, cancelHigh := smokePhaseContext()
	defer cancelHigh()
	if err := app.SwitchEffort(highCtx, "high"); err != nil {
		t.Fatalf("switch to high effort: %v", err)
	}
	highStart := len(app.Snapshot().Conversation)
	highObserver := newSmokeObserver(events)
	if err := app.Submit(highCtx, "Use plan_load exactly once. Create a three-node repository audit plan with nodes inspect, verify, and report. Do not execute the plan. Then call task_needs_user_decision with a short question that asks whether the user wants the loaded plan executed, and reply with HIGH_PLAN_SMOKE_OK. Do not call any other tool."); err != nil {
		t.Fatalf("submit voluntary high plan request: %v", err)
	}
	if err := app.WaitForIdle(highCtx); err != nil {
		highObserver.Dump(t, "high")
		t.Fatalf("wait for voluntary high plan request: %v", err)
	}
	highObserver.Dump(t, "high")
	highObserver.Close()
	high := app.Snapshot()
	highPlanLoads := 0
	highPlanEvents := make([]string, 0)
	for _, message := range high.Conversation[highStart:] {
		if message.Role == "tool" && message.Tool != nil && message.Tool.Name == "plan_load" {
			highPlanLoads++
			highPlanEvents = append(highPlanEvents, message.Role+":"+message.Tool.Status+":"+truncateSmokeReply(message.Tool.Error, 300))
			if message.Tool.Status != "success" {
				t.Fatalf("voluntary high plan_load status = %q error=%q", message.Tool.Status, truncateSmokeReply(message.Tool.Error, 500))
			}
		}
	}
	if high.Chat.Error != "" {
		t.Fatalf("voluntary high request failed: %s", high.Chat.Error)
	}
	// 观测性断言：high 是自愿规划（原注释 not a failure）——模型跳过 plan_load
	// 直接文本回复属行为波动，不作为冒烟失败；工具可用性由 medium 强验证。
	if highPlanLoads != 1 || high.Runtime.Plan == nil || len(high.Runtime.Plan.Nodes) < 3 || len(high.Runtime.Plan.Edges) < 2 {
		nodeCount, edgeCount := 0, 0
		if high.Runtime.Plan != nil {
			nodeCount, edgeCount = len(high.Runtime.Plan.Nodes), len(high.Runtime.Plan.Edges)
		}
		t.Logf("voluntary high observational: plan_loads=%d nodes=%d edges=%d events=%q (model skipped voluntary plan_load; not fatal)", highPlanLoads, nodeCount, edgeCount, highPlanEvents)
	} else {
		t.Logf("voluntary high plan_loads=%d nodes=%d edges=%d plan_run=0", highPlanLoads, len(high.Runtime.Plan.Nodes), len(high.Runtime.Plan.Edges))
	}

	// B/treatment: this is the same recovery intent through the isolated,
	// forced tool-choice path. It must succeed without executing plan_run.
	beforeTreatment := runtime.ReplanMetrics()
	replanCtx, cancelReplan := smokePhaseContext()
	defer cancelReplan()
	replan, err := runtime.PrepareReplan(replanCtx, seelebridge.ReplanRequest{
		Objective:    "Recover a failed repository inspection: create a diagnose node followed by a report node. Do not execute either node.",
		PreviousPlan: `{"entry":"inspect","nodes":{"inspect":{"input":"inspect the repository"},"report":{"input":"report findings"}},"edges":{"inspect":["report"]}}`,
		Failure:      `node "inspect": evidence was incomplete`,
		Evidence:     "node=inspect status=failed output=partial file inventory",
	})
	if err != nil {
		t.Fatalf("live replan request failed: %v", err)
	}
	if replan.Arguments == "" || !strings.Contains(replan.Result, `"status":"loaded"`) {
		t.Fatalf("live replan result = %#v, want successful plan_load", replan)
	}
	afterTreatment := runtime.ReplanMetrics()
	t.Logf("replan B/treatment forced_plan_load=true provider_requests_delta=%d accepted_delta=%d rejected_delta=%d", afterTreatment.ProviderRequests-beforeTreatment.ProviderRequests, afterTreatment.Accepted-beforeTreatment.Accepted, afterTreatment.Rejected-beforeTreatment.Rejected)

	// A/control is intentionally last and observational. Lite keeps the same
	// Plan skill but does not run forced preflight, so lack of a voluntary tool
	// call or a provider timeout is an A/B measurement, not a failure of the
	// mandatory Medium/High path or the explicit recovery treatment.
	controlCtx, cancelControl := smokePhaseContext()
	defer cancelControl()
	if err := app.SwitchEffort(controlCtx, "lite"); err != nil {
		t.Fatalf("switch to lite control: %v", err)
	}
	controlStart := len(app.Snapshot().Conversation)
	controlObserver := newSmokeObserver(events)
	if err := app.Submit(controlCtx, "A loaded plan failed at node inspect because evidence was incomplete. Create a replacement recovery plan with plan_load only. Do not call plan_run or any other tool."); err != nil {
		t.Fatalf("submit voluntary replan control: %v", err)
	}
	if err := app.WaitForIdle(controlCtx); err != nil {
		controlObserver.Dump(t, "control")
		t.Logf("replan A/control voluntary_plan_load=unknown wait_error=%q", err)
		return
	}
	controlObserver.Dump(t, "control")
	controlObserver.Close()
	control := app.Snapshot()
	controlLoaded := false
	for _, message := range control.Conversation[controlStart:] {
		if message.Tool != nil && message.Tool.Name == "plan_load" && message.Tool.Status == "success" {
			controlLoaded = true
			break
		}
	}
	t.Logf("replan A/control voluntary_plan_load=%t chat_error=%q", controlLoaded, control.Chat.Error)

	// plan_run 阶段（真实子代理执行）：plan_load 两节点 DAG 并 plan_run 执行。
	// 冒烟升级点 1：此前各阶段 plan_run=0，子代理执行链路从未真实 API 冒烟；
	// 断言节点事件时间线非空（详情页数据源）与计划收敛。
	runCtx, cancelRun := smokePhaseContext()
	defer cancelRun()
	if err := app.SwitchEffort(runCtx, "high"); err != nil {
		t.Fatalf("switch to high for plan_run: %v", err)
	}
	runStart := len(app.Snapshot().Conversation)
	runObserver := newSmokeObserver(events)
	if err := app.Submit(runCtx, "If a plan is already loaded, first call plan_clear to discard it (replacing an old plan is allowed here). Then call plan_load exactly once to load a serial two-node plan with entry node inspect (input: reply with the single word FOUND) and a second node report (input: reply with the single word REPORTED). Mark both nodes kind:agent. Then call plan_run to execute the plan. After execution, call task_complete with summary PLAN_RUN_SMOKE_OK. Do not call any other tool."); err != nil {
		t.Fatalf("submit plan_run request: %v", err)
	}
	if err := app.WaitForIdle(runCtx); err != nil {
		runObserver.Dump(t, "plan_run")
		t.Fatalf("wait for plan_run request: %v", err)
	}
	runObserver.Dump(t, "plan_run")
	runObserver.Close()
	run := app.Snapshot()
	if run.Chat.Error != "" {
		t.Fatalf("plan_run request failed: %s", run.Chat.Error)
	}
	runPlanLoads, runPlanRuns := 0, 0
	for _, message := range run.Conversation[runStart:] {
		if message.Tool == nil || message.Role != "tool" {
			continue // 只数 tool_result 消息；assistant 声明消息会翻倍计数
		}
		switch message.Tool.Name {
		case "plan_load":
			runPlanLoads++
		case "plan_run":
			runPlanRuns++
		}
	}
	// plan_loads 允许 0（模型可能复用前序阶段已加载的 plan 直接执行）；
	// plan_run 必须恰好执行一次（真实子代理执行验证）。
	if runPlanRuns != 1 {
		t.Fatalf("plan_run phase: plan_loads=%d plan_runs=%d, want exactly 1 plan_run", runPlanLoads, runPlanRuns)
	}
	if run.Runtime.Plan == nil || run.Runtime.Plan.Status != "completed" {
		t.Fatalf("plan_run phase: plan status = %+v, want completed", run.Runtime.Plan)
	}
	// 节点事件时间线（详情页数据源）：每个节点至少一条 completed 事件。
	for _, node := range run.Runtime.Plan.Nodes {
		hasTerminal := false
		for _, event := range node.Events {
			if event.Status == "completed" || event.Status == "failed" || event.Status == "skipped" {
				hasTerminal = true
				break
			}
		}
		if !hasTerminal {
			t.Fatalf("plan_run phase: node %q has no terminal event in timeline: %+v", node.ID, node.Events)
		}
	}
	t.Logf("plan_run nodes=%d events_timeline=verified plan_run=%d", len(run.Runtime.Plan.Nodes), runPlanRuns)

	// tasklist 打点阶段（真实 task_check_node）：plan_load 两节点 + 逐节点
	// task_check_node 在途打点 + task_complete 收尾。冒烟升级点 2：验证
	// 打点事件写入节点时间线（详情页入口）与终态收敛。
	listCtx, cancelList := smokePhaseContext()
	defer cancelList()
	if err := app.SwitchEffort(listCtx, "high"); err != nil {
		t.Fatalf("switch to high for tasklist: %v", err)
	}
	listStart := len(app.Snapshot().Conversation)
	listObserver := newSmokeObserver(events)
	if err := app.Submit(listCtx, "Use plan_load exactly once to load a serial two-node plan with entry node inspect (input: reply with the single word FOUND) and a second node report (input: reply with the single word REPORTED). Do NOT call plan_run. Execute the plan yourself step by step: after finishing each node call task_check_node with its node_id, then after the final node call task_complete with summary TASKLIST_SMOKE_OK. Do not call any other tool."); err != nil {
		t.Fatalf("submit tasklist request: %v", err)
	}
	if err := app.WaitForIdle(listCtx); err != nil {
		listObserver.Dump(t, "tasklist")
		t.Fatalf("wait for tasklist request: %v", err)
	}
	listObserver.Dump(t, "tasklist")
	listObserver.Close()
	list := app.Snapshot()
	if list.Chat.Error != "" {
		t.Fatalf("tasklist request failed: %s", list.Chat.Error)
	}
	listCheckNodes, listCompletes := 0, 0
	for _, message := range list.Conversation[listStart:] {
		if message.Tool == nil || message.Role != "tool" {
			continue // 只数 tool_result 消息
		}
		switch message.Tool.Name {
		case "task_check_node":
			listCheckNodes++
		case "task_complete":
			listCompletes++
		}
	}
	if listCheckNodes < 1 || listCompletes != 1 {
		t.Fatalf("tasklist phase: task_check_node=%d task_complete=%d, want >=1 check and exactly 1 complete", listCheckNodes, listCompletes)
	}
	if list.Runtime.Plan == nil || list.Runtime.Plan.Status != "completed" {
		t.Fatalf("tasklist phase: plan status = %+v, want completed", list.Runtime.Plan)
	}
	// 打点事件必须写入节点时间线（task_check_node → appendPlanNodeEvent）。
	checkedEvents := 0
	for _, node := range list.Runtime.Plan.Nodes {
		for _, event := range node.Events {
			if event.Status == "completed" {
				checkedEvents++
			}
		}
	}
	if checkedEvents < listCheckNodes {
		t.Fatalf("tasklist phase: %d completed events in timelines, want >= %d task_check_node calls", checkedEvents, listCheckNodes)
	}
	t.Logf("tasklist task_check_node=%d completed_events=%d plan=%s", listCheckNodes, checkedEvents, list.Runtime.Plan.Status)
}

// ── 过程监听（smokeObserver）────────────────────────────────
// 订阅 EventHub 事件流，把请求过程（工具调用/流式输出/错误）记录为日志行。
// 旧手段 Submit→WaitForIdle→最终快照 让过程不可见；事件订阅使"过程中发生了什么"
// 可见：失败时 Dump 输出完整过程轨迹，断言可引用工具调用序列。

type smokeObserver struct {
	sub         application.Subscription
	mu          sync.Mutex
	lines       []string
	streamChars int               // 流式输出总字符（delta 合并统计，避免逐条刷屏）
	streams     map[string]string // message_id → 流式累积内容（模型最终回复）
	done        chan struct{}
}

func newSmokeObserver(hub *application.EventHub) *smokeObserver {
	observer := &smokeObserver{sub: hub.Subscribe(256), streams: make(map[string]string), done: make(chan struct{})}
	go observer.consume()
	return observer
}

func (o *smokeObserver) consume() {
	defer close(o.done)
	for event := range o.sub.Events {
		switch event.Kind {
		case application.EventToolStarted:
			var message application.Message
			if json.Unmarshal(event.Payload, &message) == nil && message.Tool != nil {
				o.record("tool.started %s args=%s", message.Tool.Name, truncateSmokeReply(message.Tool.Arguments, 200))
			}
		case application.EventToolCompleted:
			var message application.Message
			if json.Unmarshal(event.Payload, &message) == nil && message.Tool != nil {
				errorText := ""
				if message.Tool.Error != "" {
					errorText = " error=" + truncateSmokeReply(message.Tool.Error, 200)
				}
				o.record("tool.completed %s status=%s%s duration=%v", message.Tool.Name, message.Tool.Status, errorText, message.Tool.Duration)
			}
		case application.EventMessageDelta:
			var delta application.MessageDelta
			if json.Unmarshal(event.Payload, &delta) == nil && delta.Delta != "" {
				o.mu.Lock()
				o.streamChars += len(delta.Delta)
				o.streams[delta.MessageID] += delta.Delta
				o.mu.Unlock()
			}
		case application.EventError:
			o.record("error %s", truncateSmokeReply(string(event.Payload), 300))
		case application.EventResyncRequired:
			o.record("resync.required (订阅缓冲溢出，事件被丢弃)")
		}
	}
}

func (o *smokeObserver) record(format string, args ...interface{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lines = append(o.lines, fmt.Sprintf(format, args...))
}

// Close 停止订阅并等待消费者退出（阶段结束时调用）。
func (o *smokeObserver) Close() {
	if o == nil {
		return
	}
	o.sub.Close()
	<-o.done
}

// Dump 输出本阶段的过程日志（失败诊断与断言引用）。
func (o *smokeObserver) Dump(t *testing.T, phase string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.lines) == 0 && o.streamChars == 0 && len(o.streams) == 0 {
		t.Logf("[%s] process: (no events observed)", phase)
		return
	}
	t.Logf("[%s] process: %d events, assistant.stream total=%d chars", phase, len(o.lines), o.streamChars)
	for _, line := range o.lines {
		t.Logf("[%s]   %s", phase, line)
	}
	// 模型最终回复（delta 按 message_id 累积）：跳过工具调用时的诊断关键。
	index := 0
	for _, reply := range o.streams {
		t.Logf("[%s]   assistant.reply[%d]: %q", phase, index, truncateSmokeReply(reply, 800))
		index++
	}
}

func smokePhaseContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 90*time.Second)
}

func truncateSmokeReply(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func copyOpaqueFile(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatalf("open accounts file: %v", err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create temporary accounts file: %v", err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		t.Fatalf("copy accounts file to temporary location: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatalf("close temporary accounts file: %v", err)
	}
}

// workspaceSmokeRoot 返回冒烟测试的项目根：优先仓库根（scoped 工具可读
// 真实文件），回退当前目录。
func workspaceSmokeRoot() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// smokeContextExchanger 是冒烟测试的 ContextExchanger 实现（与 main.go
// contextExchanger 同构）：ParentEvidence 从 application 镜像构造快照，
// MergeBack 排队注入（AppendSubagentContext）。
type smokeContextExchanger struct {
	app    *application.Service
	tracer provider.TraceSource
}

func (ex *smokeContextExchanger) ParentEvidence() *seelexctxsnapshot.ContextSnapshot {
	snap := ex.app.Snapshot()
	goal := ""
	for index := len(snap.Conversation) - 1; index >= 0; index-- {
		message := snap.Conversation[index]
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" {
			goal = truncateSnapshotGoal(message.Content)
			break
		}
	}
	return seelexctx.ExportSnapshotFromData(snap.Session.ID, goal, len(snap.Conversation), ex.tracer)
}

func (ex *smokeContextExchanger) MergeBack(content string) {
	ex.app.AppendSubagentContext(content)
}
