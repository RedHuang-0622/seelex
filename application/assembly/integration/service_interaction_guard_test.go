package integration

import (
	"context"
	"strings"
	"testing"
)

// TestEffortAndPluginGuardsAroundRunningSessions 覆盖 G0b 守卫：
//  1. 视图会话 running → SwitchEffort 拒绝（不触碰引擎）；
//  2. 任一会话 running → SwitchPlugin 拒绝（不触碰插件与引擎历史）；
//  3. 视图空闲、后台会话仍在跑 → effort 允许且只经 SetSystemPromptFor 写
//     视图会话引擎；plugin 仍拒绝（进程级动作）；
//  4. 全部空闲 → plugin 允许。
func TestEffortAndPluginGuardsAroundRunningSessions(t *testing.T) {
	engine := newRecordingPromptEngine()
	service := newLegacyTestService(t, engine)
	ctx := context.Background()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	bID := "sess-guard-B"
	engine.register(bID)
	if err := service.components.submitApplication.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatalf("submit B: %v", err)
	}
	waitChatStarted(t, engine.started[bID])

	// 视图会话 A 运行中：effort 变更必须被拒绝，且不触碰引擎。
	_, _, loopsBefore, clearsBefore := engine.promptSnapshot()
	if err := service.components.policyApplication.SwitchEffort("lite"); err == nil {
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
	if err := service.components.policyApplication.SwitchPlugin(ctx, "code"); err == nil {
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
	if err := service.components.policyApplication.SwitchEffort("lite"); err != nil {
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
	if err := service.components.policyApplication.SwitchPlugin(ctx, "code"); err == nil {
		t.Fatal("SwitchPlugin must remain rejected while the background session B is running")
	}

	// 全部空闲后 plugin 允许。
	close(engine.release[bID])
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.components.policyApplication.SwitchPlugin(ctx, "code"); err != nil {
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
	service := newLegacyTestService(t, engine)
	ctx := context.Background()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
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
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	if err := service.submitCommand(ctx, "/effort medium"); err != nil {
		t.Fatalf("/effort while idle: %v", err)
	}
	if got := service.effortManager.Current(); got != "medium" {
		t.Fatalf("effort after command = %q, want medium", got)
	}
}

// TestPluginSwitchPreservesEffort：插件切换只换 prompt 前缀，不得顺手把用户的
// effort 强度退回默认值。
//
// 旧实现每次切插件都 new 一个 EffortManager（默认 high）再 Apply 它的 Current()，
// 结果是"切了插件，强度悄悄变回 High"——用户把 effort 当会话级设置，这属于丢配置。
func TestPluginSwitchPreservesEffort(t *testing.T) {
	engine := newRecordingPromptEngine()
	service := newLegacyTestService(t, engine)
	ctx := context.Background()

	if err := service.components.policyApplication.SwitchEffort("max"); err != nil {
		t.Fatalf("SwitchEffort(max): %v", err)
	}
	if current, _ := service.Deps.Plugins.Current(); current.Name != "default" {
		t.Fatalf("precondition plugin = %q, want default", current.Name)
	}

	if err := service.components.policyApplication.SwitchPlugin(ctx, "code"); err != nil {
		t.Fatalf("SwitchPlugin(code): %v", err)
	}
	if got := service.effortManager.Current(); got != "max" {
		t.Fatalf("effort after plugin switch = %q, want max (插件切换不得重置强度)", got)
	}
	if got := service.components.frontendSnapshot.Snapshot().Runtime.Effort; got != "max" {
		t.Fatalf("snapshot runtime effort after plugin switch = %q, want max", got)
	}
	if prompt := service.promptStack.Render(); !strings.Contains(prompt, `<effort name="max">`) {
		t.Fatalf("插件切换后 system prompt 丢了 effort 层: %q", prompt)
	}

	// 停用插件（off）同样不得重置强度。
	if err := service.components.policyApplication.SwitchPlugin(ctx, "off"); err != nil {
		t.Fatalf("SwitchPlugin(off): %v", err)
	}
	if got := service.effortManager.Current(); got != "max" {
		t.Fatalf("effort after plugin deactivate = %q, want max", got)
	}
}
