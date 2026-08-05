//go:build manualsmoke

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// TestManualSmokeRealAccountLongContextRehydration uses the real provider to
// verify the user-visible recovery scenario: a remembered identity is followed
// by several large turns, the durable session is resumed, and the opening
// question is asked again. The assertion is deliberately on the final visible
// assistant message, not on provider internals.
func TestManualSmokeRealAccountLongContextRehydration(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFile(t, accountsSource, accountsPath)
	harness := newFullChainHarness(t, accountsPath, projectRoot, 45*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	submit := func(prompt string) {
		t.Helper()
		if err := harness.app.Submit(ctx, prompt); err != nil {
			t.Fatal(err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("live long-context turn did not become idle: %v", err)
		}
		if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
			t.Fatalf("live long-context turn failed: %s", snapshot.Chat.Error)
		}
	}

	submit("请记住：我的名字是 hzr。只回复‘已记住’，不要调用任何工具。")
	for round := 1; round <= 6; round++ {
		filler := strings.Repeat("这是长上下文压力测试材料，必须保持会话连续性。", 320)
		submit("这是长上下文压力测试第 " + string(rune('0'+round)) + " 轮。请只回复‘本轮已记录’，不要调用工具。材料如下：\n" + filler)
	}

	snapshot := harness.app.Snapshot()
	if snapshot.Session.ID == "" {
		t.Fatal("live session did not receive a durable session ID")
	}
	if err := harness.app.ResumeSession(snapshot.Session.ID); err != nil {
		t.Fatalf("resume live session: %v", err)
	}
	submit("回到最初那条消息，只回答我的名字，不要调用任何工具。")

	final := latestVisibleAssistant(snapshotAfterResume(harness.app))
	if !strings.Contains(strings.ToLower(final), "hzr") {
		t.Fatalf("real long-context answer = %q, want it to retain the opening identity", final)
	}
}

func snapshotAfterResume(app *application.Service) application.Snapshot {
	return app.Snapshot()
}

func latestVisibleAssistant(snapshot application.Snapshot) string {
	for index := len(snapshot.Conversation) - 1; index >= 0; index-- {
		message := snapshot.Conversation[index]
		if message.Role == "assistant" && message.Tool == nil && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}
