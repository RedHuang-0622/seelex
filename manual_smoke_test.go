//go:build manualsmoke

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
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

	mediumCtx, cancelMedium := smokePhaseContext()
	defer cancelMedium()
	if err := app.SwitchEffort(mediumCtx, "medium"); err != nil {
		t.Fatalf("switch to medium effort: %v", err)
	}
	if err := app.Submit(mediumCtx, "#plan"); err != nil {
		t.Fatalf("activate plan skill: %v", err)
	}
	if err := app.Submit(mediumCtx, "Use plan_load exactly once. Load a serial two-node plan with entry node inspect and a second node report; each node must have a short input. Do not execute the plan. Then call task_needs_user_decision with a short question that asks whether the user wants the loaded plan executed, and reply with PLAN_SMOKE_OK. Do not call any other tool."); err != nil {
		t.Fatalf("submit live plan request: %v", err)
	}
	if err := app.WaitForIdle(mediumCtx); err != nil {
		t.Fatalf("wait for live plan request: %v", err)
	}

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
	if err := app.Submit(highCtx, "Use plan_load exactly once. Create a three-node repository audit plan with nodes inspect, verify, and report. Do not execute the plan. Then call task_needs_user_decision with a short question that asks whether the user wants the loaded plan executed, and reply with HIGH_PLAN_SMOKE_OK. Do not call any other tool."); err != nil {
		t.Fatalf("submit voluntary high plan request: %v", err)
	}
	if err := app.WaitForIdle(highCtx); err != nil {
		t.Fatalf("wait for voluntary high plan request: %v", err)
	}
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
	if err := app.Submit(controlCtx, "A loaded plan failed at node inspect because evidence was incomplete. Create a replacement recovery plan with plan_load only. Do not call plan_run or any other tool."); err != nil {
		t.Fatalf("submit voluntary replan control: %v", err)
	}
	if err := app.WaitForIdle(controlCtx); err != nil {
		t.Logf("replan A/control voluntary_plan_load=unknown wait_error=%q", err)
		return
	}
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
