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
//	go test -tags manualsmoke . -run TestManualSmokeRealAccountPlan -count=1 -timeout=2m
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

	appEngine := newEnginePort(frameworkEngine)
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
	runtime.SetPlanBranchCallback(app.HandlePlanBranchEvent)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := app.SwitchEffort(ctx, "medium"); err != nil {
		t.Fatalf("switch to medium effort: %v", err)
	}
	if err := app.Submit(ctx, "#plan"); err != nil {
		t.Fatalf("activate plan skill: %v", err)
	}
	if err := app.Submit(ctx, "Use plan_load exactly once. Load a serial two-node plan with entry node inspect and a second node report; each node must have a short input. Do not run the plan and do not call any other tool. After a successful load, reply with PLAN_SMOKE_OK."); err != nil {
		t.Fatalf("submit live plan request: %v", err)
	}
	if err := app.WaitForIdle(ctx); err != nil {
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

	replan, err := runtime.PrepareReplan(ctx, seelebridge.ReplanRequest{
		Objective:    "Inspect and report the repository safely.",
		PreviousPlan: `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"}},"edges":{}}`,
		Failure:      `node "inspect": simulated incomplete evidence`,
		Evidence:     "node=inspect status=failed",
	})
	if err != nil {
		t.Fatalf("live replan request failed: %v", err)
	}
	if replan.Arguments == "" || !strings.Contains(replan.Result, `"status":"loaded"`) {
		t.Fatalf("live replan result = %#v, want successful plan_load", replan)
	}
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
