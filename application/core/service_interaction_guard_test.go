package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingPromptEngine 记录 per-session prompt / 全局 prompt / maxLoops /
// clearHistory 调用，用于断言 effort 与 plugin 守卫（G0b）没有越过程序级
// 边界触达运行中的会话引擎。
type recordingPromptEngine struct {
	*multiSessionEngine
	mu                sync.Mutex
	promptFor         map[string]string
	globalPrompts     []string
	maxLoopsCalls     int
	clearHistoryCalls int
}

func newRecordingPromptEngine() *recordingPromptEngine {
	return &recordingPromptEngine{
		multiSessionEngine: newMultiSessionEngine(),
		promptFor:          map[string]string{},
	}
}

func (engine *recordingPromptEngine) SetSystemPromptFor(sessionID, prompt string) {
	engine.mu.Lock()
	engine.promptFor[sessionID] = prompt
	engine.mu.Unlock()
}

func (engine *recordingPromptEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.globalPrompts = append(engine.globalPrompts, prompt)
	engine.mu.Unlock()
}

func (engine *recordingPromptEngine) SetMaxLoops(loops int) {
	engine.mu.Lock()
	engine.maxLoopsCalls++
	engine.mu.Unlock()
}

func (engine *recordingPromptEngine) ClearHistory() {
	engine.mu.Lock()
	engine.clearHistoryCalls++
	engine.mu.Unlock()
}

// promptSnapshot 返回测试断言的引擎侧快照（加锁拷贝）。
func (engine *recordingPromptEngine) promptSnapshot() (promptFor map[string]string, globalCount, loops, clears int) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	promptFor = make(map[string]string, len(engine.promptFor))
	for id, prompt := range engine.promptFor {
		promptFor[id] = prompt
	}
	return promptFor, len(engine.globalPrompts), engine.maxLoopsCalls, engine.clearHistoryCalls
}

// waitChatStarted 等待指定会话的 ChatStream 进入引擎（阻塞点已建立）。
func waitChatStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not start")
	}
}

// waitUnitIdle 轮询指定会话单元直到其聊天停止运行（后台另一会话可能仍在跑，
// 因此不能使用 WaitForIdle）。
func waitUnitIdle(t *testing.T, service *Service, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		service.ViewMu.RLock()
		running := false
		if unit := service.sessions.Unit(sessionID); unit != nil {
			running = unit.ChatState().Running
		}
		service.ViewMu.RUnlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("session did not become idle within deadline")
}

// TestEffortAndPluginGuardsAroundRunningSessions 覆盖 G0b 守卫：
//  1. 视图会话 running → SwitchEffort 拒绝（不触碰引擎）；
//  2. 任一会话 running → SwitchPlugin 拒绝（不触碰插件与引擎历史）；
//  3. 视图空闲、后台会话仍在跑 → effort 允许且只经 SetSystemPromptFor 写
//     视图会话引擎；plugin 仍拒绝（进程级动作）；
//  4. 全部空闲 → plugin 允许。
func TestEffortAndPluginGuardsAroundRunningSessions(t *testing.T) {
	engine := newRecordingPromptEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	bID := "sess-guard-B"
	engine.register(bID)
	if err := service.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatalf("submit B: %v", err)
	}
	waitChatStarted(t, engine.started[bID])

	// 视图会话 A 运行中：effort 变更必须被拒绝，且不触碰引擎。
	_, _, loopsBefore, clearsBefore := engine.promptSnapshot()
	if err := service.SwitchEffort(ctx, "lite"); err == nil {
		t.Fatal("SwitchEffort must be rejected while the view session is running")
	}
	if got := service.effortManager.Current(); got != "high" {
		t.Fatalf("effort after rejected switch = %q, want high", got)
	}
	_, _, loopsAfter, _ := engine.promptSnapshot()
	if loopsAfter != loopsBefore {
		t.Fatalf("SetMaxLoops called by rejected effort switch: before=%d after=%d", loopsBefore, loopsAfter)
	}

	// 任一会话 running：plugin 切换必须被拒绝，且不触碰插件/引擎历史。
	if err := service.SwitchPlugin(ctx, "code"); err == nil {
		t.Fatal("SwitchPlugin must be rejected while any session is running")
	}
	if current, _ := service.Deps.Plugins.Current(); current.Name != "default" {
		t.Fatalf("plugin changed by rejected switch: %q", current.Name)
	}
	_, _, _, clearsAfter := engine.promptSnapshot()
	if clearsAfter != clearsBefore {
		t.Fatalf("ClearHistory called by rejected plugin switch: before=%d after=%d", clearsBefore, clearsAfter)
	}

	// 释放 A：视图转 idle，B 继续后台运行。
	close(engine.release[aID])
	waitUnitIdle(t, service, aID)

	// effort 只作用于 idle 视图会话：允许，且写的是 per-session prompt。
	promptForBefore, globalBefore, _, _ := engine.promptSnapshot()
	if err := service.SwitchEffort(ctx, "lite"); err != nil {
		t.Fatalf("SwitchEffort rejected while view is idle (background B running): %v", err)
	}
	promptFor, globalCount, _, _ := engine.promptSnapshot()
	if want := service.promptStack.Render(); promptFor[aID] != want {
		t.Fatalf("view prompt after effort switch = %q, want %q", promptFor[aID], want)
	}
	if globalCount != globalBefore || len(promptFor) != len(promptForBefore) {
		t.Fatalf("effort switch must only write the view session via SetSystemPromptFor: global %d→%d, per-session %d→%d",
			globalBefore, globalCount, len(promptForBefore), len(promptFor))
	}

	// plugin 仍是进程级动作：B 在跑 → 拒绝。
	if err := service.SwitchPlugin(ctx, "code"); err == nil {
		t.Fatal("SwitchPlugin must remain rejected while the background session B is running")
	}

	// 全部空闲后 plugin 允许。
	close(engine.release[bID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.SwitchPlugin(ctx, "code"); err != nil {
		t.Fatalf("SwitchPlugin rejected while idle: %v", err)
	}
	if current, _ := service.Deps.Plugins.Current(); current.Name != "code" {
		t.Fatalf("plugin after idle switch = %q, want code", current.Name)
	}
}

// TestEffortCommandUsesGuardedServicePath /effort 命令必须走 SwitchEffort：
// 视图会话运行中命令返回错误而不是绕过守卫直接改引擎。
func TestEffortCommandUsesGuardedServicePath(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	if err := service.submitCommand(ctx, "/effort lite"); err == nil {
		t.Fatal("/effort while running must fail")
	}
	if got := service.effortManager.Current(); got != "high" {
		t.Fatalf("effort after rejected command = %q, want high", got)
	}
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	if err := service.submitCommand(ctx, "/effort medium"); err != nil {
		t.Fatalf("/effort while idle: %v", err)
	}
	if got := service.effortManager.Current(); got != "medium" {
		t.Fatalf("effort after command = %q, want medium", got)
	}
}
