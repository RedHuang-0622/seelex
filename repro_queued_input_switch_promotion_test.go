package main

// 全链路回归：会话 A 运行中接收一条输入（进 A 自己的消息队列、尚未发送），随后
//
//	① 切换到另一个正在运行的会话 B；
//	② 再切换到没有正在运行的会话 C；
//	③ 放行 A 的本轮 → A 队列里的输入必须被整批提升为下一轮（真的发出去）。
//
// 观测：队列里有东西但没人发送 = 红灯；A 的下一轮出现在 provider 上 = 绿灯。
// 这条用例跑的是真实 EnginePort + 真实会话存储（不是包内桩），钉的就是
// "运行中收下的输入在切换会话之后仍会被提升发送"这一条生产路径；
// 迭代边界注入撞会话锁的那次自锁死见
// application/core/goal_directive_session_lock_test.go。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReproQueuedInputThenSwitchSessions(t *testing.T) {
	provider := newPerRequestGateProvider(3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		provider.serve(t, writer, request)
	}))
	defer server.Close()
	t.Cleanup(provider.releaseAll)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-3\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-4\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	submitAndIdle := func(ctx context.Context, text string) string {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
		}
		return harness.app.Snapshot().Session.ID
	}

	sessionA := submitAndIdle(ctx, "seed A") // provider #1
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if got := submitAndIdle(ctx, "seed B"); got != sessionB {
		t.Fatalf("seed B active = %q, want %q", got, sessionB)
	} // provider #2
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatalf("fork C: %v", err)
	}
	if got := submitAndIdle(ctx, "seed C"); got != sessionC {
		t.Fatalf("seed C active = %q, want %q", got, sessionC)
	} // provider #3

	// 会话 A 起一轮长回合（provider #4 阻塞）。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long A"); err != nil {
		t.Fatalf("submit long A: %v", err)
	}
	provider.waitBlocked(t, ctx, 4)

	// 消息队列里有东西没发出去：往运行中的 A 再发一条（显式路由到 A）。
	if err := harness.app.SubmitToSession(ctx, sessionA, "queued for A"); err != nil {
		t.Fatalf("submit queued for A: %v", err)
	}
	snapA, err := harness.app.SnapshotOf(sessionA)
	if err != nil {
		t.Fatalf("SnapshotOf(A): %v", err)
	}
	if snapA.Chat.QueuedCount != 1 || len(snapA.Chat.InputQueue) != 1 {
		t.Fatalf("A 排队投影 = %+v, want 1 条待发", snapA.Chat)
	}

	// ① 切换到另一个正在运行的会话 B（provider #5 阻塞）。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B: %v", err)
	}
	if err := harness.app.Submit(ctx, "long B"); err != nil {
		t.Fatalf("submit long B: %v", err)
	}
	provider.waitBlocked(t, ctx, 5)

	// ② 切换到没有正在运行的会话 C（A、B 都在跑 → 异步冷加载）。
	switchDone := make(chan error, 1)
	started := time.Now()
	go func() { switchDone <- harness.app.ResumeSession(sessionC) }()
	select {
	case err := <-switchDone:
		if err != nil {
			t.Fatalf("ResumeSession(C) = %v", err)
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("ResumeSession(C) took %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("RED: ResumeSession(C) 卡住 >3s\n%s", allGoroutineStacks())
	}

	// ③ 放行 A 的本轮：A 队列里的输入必须被提升为下一轮（= 新的 provider 请求）。
	provider.release(4)
	sent := make(chan int, 1)
	go func() {
		for number := range provider.seen {
			sent <- number
			return
		}
	}()
	select {
	case number := <-sent:
		t.Logf("队列提升发出新请求 #%d", number)
	case <-time.After(15 * time.Second):
		snapA, _ = harness.app.SnapshotOf(sessionA)
		t.Fatalf("RED: A 队列里的输入没有发出去（A.Chat=%+v，provider %s）\n%s",
			snapA.Chat, provider.debug(), allGoroutineStacks())
	}

	provider.releaseAll()
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle: %v\n%s", err, allGoroutineStacks())
	}
	snapA, err = harness.app.SnapshotOf(sessionA)
	if err != nil {
		t.Fatalf("SnapshotOf(A) after idle: %v", err)
	}
	if snapA.Chat.QueuedCount != 0 || len(snapA.Chat.InputQueue) != 0 {
		t.Fatalf("A 队列未排空：%+v", snapA.Chat)
	}
	if text := conversationText(snapA.Conversation); !containsAll(text, "queued for A") {
		t.Fatalf("A 的可见会话里没有排队输入：%q", text)
	}
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
