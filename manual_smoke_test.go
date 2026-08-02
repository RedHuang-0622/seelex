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

	originalStorePath := *storePath
	*storePath = filepath.Join(tempDir, "sessions")
	defer func() { *storePath = originalStorePath }()

	skills := initSkillSystem()
	plugins := initPluginSystem(runtime, skills)
	hooks := application.NewToolHookBridge()
	frameworkEngine := initEngine(runtime, hooks)
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	runtime.SetPlanApprovalGate(&planApprovalGate{broker: approval})
	activateDefaultPlugin(plugins, frameworkEngine)

	appEngine := newEnginePort(frameworkEngine, func() reactorEngine {
		return initEngine(runtime, hooks)
	}, runtime.Tracer())
	store := initStore()
	defer store.Close()
	app := initApplication(
		appEngine,
		runtime,
		plugins,
		initSessionManager(store, appEngine),
		skills,
		initWorkspaceRepo(),
		events,
		approval,
	)
	defer app.Shutdown()
	hooks.Bind(app)
	runtime.SetPlanNodeCallback(app.HandlePlanNodeComplete)
	// 子代理上下文闭环（与 main.go 同款）：父证据注入 + 执行后 merge-back。
	runtime.SetNodeParentEvidence(func() *seelexctxsnapshot.ContextSnapshot {
		current := runtime.CurrentSession()
		if current == nil {
			return nil
		}
		return seelexctx.ExportSnapshot(current, runtime.Tracer(), "")
	})

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
	if high.Chat.Error != "" || highPlanLoads != 1 || high.Runtime.Plan == nil || len(high.Runtime.Plan.Nodes) < 3 || len(high.Runtime.Plan.Edges) < 2 {
		nodeCount, edgeCount := 0, 0
		if high.Runtime.Plan != nil {
			nodeCount, edgeCount = len(high.Runtime.Plan.Nodes), len(high.Runtime.Plan.Edges)
		}
		t.Fatalf("voluntary high result: plan_loads=%d nodes=%d edges=%d chat_error=%q events=%q, want one successful three-node DAG", highPlanLoads, nodeCount, edgeCount, high.Chat.Error, highPlanEvents)
	}
	t.Logf("voluntary high plan_loads=%d nodes=%d edges=%d plan_run=0", highPlanLoads, len(high.Runtime.Plan.Nodes), len(high.Runtime.Plan.Edges))

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
}

// ── 过程监听（smokeObserver）────────────────────────────────
// 订阅 EventHub 事件流，把请求过程（工具调用/流式输出/错误）记录为日志行。
// 旧手段 Submit→WaitForIdle→最终快照 让过程不可见；事件订阅使"过程中发生了什么"
// 可见：失败时 Dump 输出完整过程轨迹，断言可引用工具调用序列。

type smokeObserver struct {
	sub         application.Subscription
	mu          sync.Mutex
	lines       []string
	streamChars int // 流式输出总字符（delta 合并统计，避免逐条刷屏）
	done        chan struct{}
}

func newSmokeObserver(hub *application.EventHub) *smokeObserver {
	observer := &smokeObserver{sub: hub.Subscribe(256), done: make(chan struct{})}
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
	if len(o.lines) == 0 && o.streamChars == 0 {
		t.Logf("[%s] process: (no events observed)", phase)
		return
	}
	t.Logf("[%s] process: %d events, assistant.stream total=%d chars", phase, len(o.lines), o.streamChars)
	for _, line := range o.lines {
		t.Logf("[%s]   %s", phase, line)
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
