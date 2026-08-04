//go:build manualsmoke

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// TestManualSmokeRealAccountBashFullChain is an opt-in paid-provider smoke
// test. It verifies that a real streamed tool call completes under
// full_access and reaches the Application event/snapshot boundary.
func TestManualSmokeRealAccountBashFullChain(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFile(t, accountsSource, accountsPath)
	harness := newFullChainHarness(t, accountsPath, projectRoot, 30*time.Second)

	subscription := harness.events.Subscribe(256)
	defer subscription.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := harness.app.Submit(ctx, "Call the bash tool exactly once with command pwd. After it completes, report the working directory in one short sentence. Do not call any other tool."); err != nil {
		t.Fatal(err)
	}

	completed := waitForToolCompleted(t, ctx, subscription.Events, "bash")
	var completedMessage application.Message
	if err := json.Unmarshal(completed.Payload, &completedMessage); err != nil {
		t.Fatalf("decode live tool.completed payload: %v", err)
	}
	if completedMessage.Tool == nil || completedMessage.Tool.Status != "success" {
		t.Fatalf("live bash completion = %#v", completedMessage.Tool)
	}
	var result struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(completedMessage.Tool.Result), &result); err != nil {
		t.Fatalf("decode live bash result: %v", err)
	}
	if result.ExitCode != 0 || !strings.Contains(strings.ToLower(result.Stdout), strings.ToLower(projectRoot)) {
		t.Fatalf("live bash result = %+v, want project cwd %q", result, projectRoot)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("live chat did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" || snapshot.Chat.Running {
		t.Fatalf("live final chat state = %+v", snapshot.Chat)
	}
}

// TestManualSmokeRealAccountTodoFullChain reproduces the first internal tool
// used by the GUI for ordinary repository tasks. It verifies that a
// full-access launch cannot stall between tool.started and tool.completed.
func TestManualSmokeRealAccountTodoFullChain(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFile(t, accountsSource, accountsPath)
	harness := newFullChainHarness(t, accountsPath, projectRoot, 30*time.Second)

	subscription := harness.events.Subscribe(256)
	defer subscription.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := harness.app.Submit(ctx, "Call todolist_init exactly once with one item named inspect, then reply done. Do not call any other tool."); err != nil {
		t.Fatal(err)
	}

	completed := waitForToolCompleted(t, ctx, subscription.Events, "todolist_init")
	var completedMessage application.Message
	if err := json.Unmarshal(completed.Payload, &completedMessage); err != nil {
		t.Fatalf("decode live tool.completed payload: %v", err)
	}
	if completedMessage.Tool == nil || completedMessage.Tool.Status != "success" {
		t.Fatalf("live todolist completion = %#v", completedMessage.Tool)
	}
	if !strings.Contains(completedMessage.Tool.Result, `"total":1`) {
		t.Fatalf("live todolist result = %q, want one item", completedMessage.Tool.Result)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("live chat did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" || snapshot.Chat.Running {
		t.Fatalf("live final chat state = %+v", snapshot.Chat)
	}
}
