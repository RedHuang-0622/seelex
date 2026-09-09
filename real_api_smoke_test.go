//go:build manualsmoke2

package main

// TestRealAPISessionRestoreSmoke 是真实 API 会话链路冒烟（opt-in）：
// 多轮真实请求 → 进程重启 → ResumeSession 冷恢复 → 再次提问，
// 验证持久化会话在真实 provider 下可恢复且回答延续会话身份。
//
// 运行（config/accounts.yaml 内容不会被读取/打印）：
//   $env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//   go test -tags manualsmoke2 . -run 'TestRealAPISessionRestoreSmoke' \
//     -count=1 -v -cpuprofile %TEMP%\real-api.cpu -memprofile %TEMP%\real-api.mem -timeout 1500s

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

func TestRealAPISessionRestoreSmoke(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}
	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFileManual(t, accountsSource, accountsPath)
	harness := newFullChainHarness(t, accountsPath, projectRoot, 45*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	submit := func(prompt string) {
		t.Helper()
		if err := harness.app.Submit(ctx, prompt); err != nil {
			t.Fatal(err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("live turn did not become idle: %v", err)
		}
		if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
			t.Fatalf("live turn failed: %s", snapshot.Chat.Error)
		}
	}

	submit("请记住：我的名字是 hzr。只回复‘已记住’，不要调用任何工具。")
	for round := 1; round <= 4; round++ {
		filler := strings.Repeat("真实 API 会话连续性材料，不要遗漏。", 160)
		submit("这是真实 API 冒烟第 " + string(rune('0'+round)) + " 轮。请只回复‘本轮已记录’，不要调用工具。材料如下：\n" + filler)
	}
	sessionID := harness.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("live session did not receive a durable session ID")
	}
	harness.app.Shutdown()

	restarted := newFullChainHarness(t, accountsPath, projectRoot, 45*time.Second)
	if err := restarted.app.ResumeSession(sessionID); err != nil {
		t.Fatalf("restart resume live session: %v", err)
	}
	restarted.submitLive(ctx, t, "请回忆上一轮对话你回复的内容要求。只回答最近一轮你被要求回复的那句话，不要解释。")
	final := latestVisibleAssistantManual(restarted.app.Snapshot())
	if !strings.Contains(final, "本轮已记录") {
		t.Fatalf("real restore answer = %q, want it to retain the recent turn instruction", final)
	}
	restarted.app.Shutdown()
}

func (harness *fullChainHarness) submitLive(ctx context.Context, t *testing.T, prompt string) {
	t.Helper()
	if err := harness.app.Submit(ctx, prompt); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("live resume turn did not become idle: %v", err)
	}
}

func latestVisibleAssistantManual(snapshot application.Snapshot) string {
	for index := len(snapshot.Conversation) - 1; index >= 0; index-- {
		message := snapshot.Conversation[index]
		if message.Role == "assistant" && message.Tool == nil && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}

func copyOpaqueFileManual(t *testing.T, source, dest string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read accounts source: %v", err)
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		t.Fatalf("write accounts copy: %v", err)
	}
}
