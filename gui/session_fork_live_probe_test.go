package gui

// TestRealAPISessionForkLiveProbe 是**会话分叉（ForkSession）**的真实 API 冒烟：
// 现有 fork 探针覆盖的是子代理 fork（`fork_subagents`），会话分叉此前没有真机
// 证据，而它在 2026-08-31→09-06→09-11 之间反复回归过。
//
// 场景（对应当前唯一能钉住它的离线 repro：repro_two_running_view_third_then_switched_finishes_test.go）：
//
//	1. A 落一轮（分叉要继承的前缀）；
//	2. 从 A 分叉出 B，B 落一轮；从 B 分叉出 C，C 落一轮；
//	3. 切回 A 发起一个长任务（不等待），期间切到 C 再落一轮——A 的收尾与 C 的
//	   运行并发；
//	4. 等全部收敛后切回 C，断言：
//	   - C 自己的两轮都在（"C1/C2" 标记）——历史上这里丢过 C 的第一轮；
//	   - 继承前缀还在（A 的 seed 标记）——F-4 契约：分叉子会话必须可见继承内容；
//	   - A 的在途内容不在 C 里（"A-INFLIGHT" 标记）——真污染判据。
//
// 运行（真实 API，默认跳过）：
//
//	go build -tags pprof -o tmp/headless-smoke/seelex-pprof.exe .
//	$env:SMOKE_SESSION_FORK_LIVE='1'; $env:SMOKE_SESSION_FORK_LIVE_PPROF='1'
//	go test ./gui -run TestRealAPISessionForkLiveProbe -v -count=1 -timeout 30m
//
// 可选 env：
//
//	SMOKE_SESSION_FORK_LIVE_TARGET     目标二进制（默认 tmp/headless-smoke/seelex-pprof.exe）
//	SMOKE_SESSION_FORK_LIVE_KEEP_STORE 置 1 保留临时数据根

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/model"
)

func TestRealAPISessionForkLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_SESSION_FORK_LIVE") == "" {
		t.Skip("set SMOKE_SESSION_FORK_LIVE=1 to run the real-API session fork live probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	usePprof := os.Getenv("SMOKE_SESSION_FORK_LIVE_PPROF") == "1"
	defaultTarget := filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe")
	if usePprof {
		defaultTarget = filepath.Join(repoRoot, "tmp", "headless-smoke", "seelex-pprof.exe")
	}
	target := forkLiveEnvPath("SMOKE_SESSION_FORK_LIVE_TARGET", defaultTarget)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先构建含 ForkSessionLatest 的 headless 二进制）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-session-fork-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_SESSION_FORK_LIVE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()
	port := forkLiveFreePort(t)
	pprofAddr := ""
	if usePprof {
		pprofAddr = "127.0.0.1:" + forkLiveFreePort(t)
	}
	proc := forkLiveSpawn(t, target, repoRoot, storeDir, port, pprofAddr)
	defer proc.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	proc.waitHealthy(ctx, t, time.Now().Add(90*time.Second))

	submitIdle := func(prompt string) string {
		t.Helper()
		if _, err := proc.rpc(ctx, "Submit", prompt); err != nil {
			t.Fatalf("Submit(%q): %v", prompt, err)
		}
		if _, err := proc.rpc(ctx, "WaitIdle", 300); err != nil {
			t.Fatalf("WaitIdle after %q: %v", prompt, err)
		}
		return roleLiveSessionID(t, ctx, proc)
	}
	fork := func(parentID string) string {
		t.Helper()
		raw, err := proc.rpc(ctx, "ForkSessionLatest", parentID)
		if err != nil {
			t.Fatalf("ForkSessionLatest(%s): %v", parentID, err)
		}
		var childID string
		if err := json.Unmarshal(raw, &childID); err != nil {
			t.Fatalf("decode ForkSessionLatest(%s): %v", parentID, err)
		}
		if childID == "" {
			t.Fatalf("ForkSessionLatest(%s) 返回空子会话 ID", parentID)
		}
		return childID
	}

	// 1) A 落一轮：后面的分叉都应继承这段前缀。
	sessionA := submitIdle("SFF-SEED 只回复 OK，不要调用任何工具。")
	// 2) A → B → C，各自落一轮自己的标记。
	sessionB := fork(sessionA)
	if got := submitIdle("SFF-B1 只回复 OK。"); got != sessionB {
		t.Fatalf("B 轮次落到了 %q，want %q", got, sessionB)
	}
	sessionC := fork(sessionB)
	if got := submitIdle("SFF-C1 只回复 OK。"); got != sessionC {
		t.Fatalf("C 第一轮落到了 %q，want %q", got, sessionC)
	}

	// 3) 切回 A 发起长任务（不等待），期间切到 C 再落一轮——制造并发窗口。
	if _, err := proc.rpc(ctx, "ResumeSession", sessionA); err != nil {
		t.Fatalf("ResumeSession(A): %v", err)
	}
	if _, err := proc.rpc(ctx, "Submit", "SFF-A-INFLIGHT 分章节写一篇 600 字以上的说明文，写完用一句话总结。"); err != nil {
		t.Fatalf("Submit(A long): %v", err)
	}
	if _, err := proc.rpc(ctx, "ResumeSession", sessionC); err != nil {
		t.Fatalf("ResumeSession(C): %v", err)
	}
	if _, err := proc.rpc(ctx, "Submit", "SFF-C2 只回复 OK。"); err != nil {
		t.Fatalf("Submit(C second): %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 600); err != nil {
		t.Fatalf("WaitIdle(all): %v", err)
	}

	// 4) 切回 C 验内容。
	if _, err := proc.rpc(ctx, "ResumeSession", sessionC); err != nil {
		t.Fatalf("ResumeSession(C) final: %v", err)
	}
	text := sessionForkLiveConversation(t, ctx, proc)
	for _, want := range []string{"SFF-SEED", "SFF-C1", "SFF-C2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("C 会话缺少 %s（自身轮次/继承前缀丢失）: %q", want, text)
		}
	}
	if strings.Contains(text, "SFF-A-INFLIGHT") {
		t.Fatalf("C 会话被 A 的在途内容污染: %q", text)
	}
	t.Logf("[session-fork] C 会话完整：继承前缀 + 自身两轮都在，且无 A 在途污染（A=%s B=%s C=%s）",
		sessionA, sessionB, sessionC)
}

func sessionForkLiveConversation(t *testing.T, ctx context.Context, proc *forkLiveProc) string {
	t.Helper()
	raw, err := proc.rpc(ctx, "Snapshot")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var snapshot model.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode Snapshot: %v", err)
	}
	var builder strings.Builder
	for _, message := range snapshot.Conversation {
		builder.WriteString(message.Role)
		builder.WriteString(":")
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}
