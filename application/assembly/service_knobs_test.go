package assembly

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// composer 底部两条「独立拉条」的落点测试（service_knobs.go）。
//
// 这里钉住的是三件事，缺一条"独立拉条"就是假的：
//  1. 各改一项：SetLoopLimit 只写引擎的循环上限，SetReasoningEffort 只写账号池的
//     wire 思考强度（互不连带）；
//  2. 档位是**预设**：SwitchEffort 把两条读数一起带去该档（前端因此看得见档位改
//     了什么）；
//  3. 运行守卫与档位同一条：视图会话 running 时两条都拒绝，且不触碰引擎/账号池。
//
// 不在这里测的：真实账号池"只改跟随会话的账号"（backend/engine/account 的用例）、
// 真实引擎的循环语义（Seele 侧）。

func knobService(t *testing.T) (*Service, *fakeEngine) {
	t.Helper()
	engine := &fakeEngine{}
	return newTestService(t, engine), engine
}

func runtimeKnobs(service *Service) (string, int) {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.Core.Snapshot.Runtime.ReasoningEffort, service.Core.Snapshot.Runtime.LoopLimit
}

// TestSetLoopLimitOnlyTouchesLoopLimit：改循环次数不改思考强度，且引擎与快照一致。
func TestSetLoopLimitOnlyTouchesLoopLimit(t *testing.T) {
	service, engine := knobService(t)
	if err := frontendOfTestService(service).SetLoopLimit(context.Background(), 600); err != nil {
		t.Fatalf("SetLoopLimit(600): %v", err)
	}
	if engine.maxLoops != 600 {
		t.Fatalf("engine maxLoops = %d, want 600", engine.maxLoops)
	}
	rounds, loops := runtimeKnobs(service)
	if loops != 600 {
		t.Fatalf("snapshot loop limit = %d, want 600", loops)
	}
	// 装配期已经把 high 档的 low/medium/high/max 派生值播种进去了（reasoning 默认
	// high）——改循环次数不该动它。
	if rounds != dto.ReasoningEffortHigh {
		t.Fatalf("snapshot reasoning effort = %q, want high（不该被连带写入）", rounds)
	}
}

// TestSetLoopLimitRejectsOutOfRange：上界 = limits.input_loop_limit（9999 是 goal
// 用的"≈无上限"哨兵），越界在写引擎**之前**就被拒。
func TestSetLoopLimitRejectsOutOfRange(t *testing.T) {
	service, engine := knobService(t)
	before := engine.maxLoops
	for _, limit := range []int{0, -1, 10000, 1 << 20} {
		if err := frontendOfTestService(service).SetLoopLimit(context.Background(), limit); err == nil {
			t.Fatalf("SetLoopLimit(%d) 应当被拒", limit)
		}
	}
	if engine.maxLoops != before {
		t.Fatalf("被拒的调用不该触碰引擎：maxLoops = %d → %d", before, engine.maxLoops)
	}
	if _, loops := runtimeKnobs(service); loops != prompt.MaxLoops("high") {
		t.Fatalf("被拒的调用不该改快照：loop limit = %d（装配期播种的 high=384）", loops)
	}
}

// TestSetReasoningEffortOnlyTouchesReasoningEffort：改思考强度只推一次 wire 值，
// 循环上限一个字都不动。
func TestSetReasoningEffortOnlyTouchesReasoningEffort(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &effortWireRuntime{fakeRuntime: &fakeRuntime{}}
	service := newTestService(t, engine, withTestRuntime(runtime))
	if err := frontendOfTestService(service).SetLoopLimit(context.Background(), 600); err != nil {
		t.Fatalf("SetLoopLimit(600): %v", err)
	}
	loopsBefore := engine.maxLoops

	if err := frontendOfTestService(service).SetReasoningEffort(context.Background(), "  HIGH "); err != nil {
		t.Fatalf("SetReasoningEffort(high): %v", err)
	}
	pushed := serviceRuntimePushes(t, service)
	if len(pushed) != 1 || pushed[0] != dto.ReasoningEffortHigh {
		t.Fatalf("pushed = %v, want exactly [high]", pushed)
	}
	rounds, loops := runtimeKnobs(service)
	if rounds != dto.ReasoningEffortHigh {
		t.Fatalf("snapshot reasoning effort = %q, want high", rounds)
	}
	if loops != 600 {
		t.Fatalf("循环上限被连带改掉了：%d，want 600", loops)
	}
	if engine.maxLoops != loopsBefore {
		t.Fatalf("SetReasoningEffort 不该碰引擎：%d → %d", loopsBefore, engine.maxLoops)
	}
}

// TestSetReasoningEffortRejectsUnknownVocabulary：非法值（含 "session"——那是"没有
// 覆盖"的同义词，不是拉条能停的位置）在推之前就被拒。
func TestSetReasoningEffortRejectsUnknownVocabulary(t *testing.T) {
	service, _ := newEffortWireService(t)
	for _, value := range []string{"turbo", "session", "HIGHEST", "", "低"} {
		if err := frontendOfTestService(service).SetReasoningEffort(context.Background(), value); err == nil {
			t.Fatalf("SetReasoningEffort(%q) 应当被拒", value)
		}
	}
	if pushed := serviceRuntimePushes(t, service); len(pushed) != 0 {
		t.Fatalf("被拒的调用不该下发：%v", pushed)
	}
	if rounds, _ := runtimeKnobs(service); rounds != dto.ReasoningEffortHigh {
		t.Fatalf("被拒的调用不该改快照：%q（装配期播种的是 high）", rounds)
	}
}

// TestSwitchEffortSeedsBothKnobReadings：档位是两条拉条的**预设**——切一次档位，
// 两条读数一起落到该档（lite → low / 15）。
func TestSwitchEffortSeedsBothKnobReadings(t *testing.T) {
	service, engine := knobService(t)
	if err := frontendOfTestService(service).SwitchEffort(context.Background(), "lite"); err != nil {
		t.Fatalf("SwitchEffort(lite): %v", err)
	}
	if engine.maxLoops != 15 {
		t.Fatalf("engine maxLoops = %d, want 15（lite 档默认）", engine.maxLoops)
	}
	rounds, loops := runtimeKnobs(service)
	if rounds != dto.ReasoningEffortLow || loops != 15 {
		t.Fatalf("档位切换后的两条读数 = (%q, %d), want (low, 15)", rounds, loops)
	}
}

// TestKnobsAreGuardedWhileViewSessionRuns：视图会话 running 时两条拉条都拒绝，且
// 引擎与账号池一个字节都不动（前端的置灰只是体验，后端必须自己挡）。
func TestKnobsAreGuardedWhileViewSessionRuns(t *testing.T) {
	engine := newRecordingPromptEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	sessionID := engine.SessionID()
	waitChatStarted(t, engine.started[sessionID])

	_, _, loopsBefore, _ := engine.promptSnapshot()
	roundsBefore, limitBefore := runtimeKnobs(service)
	if err := frontendOfTestService(service).SetLoopLimit(ctx, 600); err == nil {
		t.Fatal("SetLoopLimit 在视图会话 running 时必须被拒")
	}
	if err := frontendOfTestService(service).SetReasoningEffort(ctx, dto.ReasoningEffortMax); err == nil {
		t.Fatal("SetReasoningEffort 在视图会话 running 时必须被拒")
	}
	_, _, loopsAfter, _ := engine.promptSnapshot()
	if loopsAfter != loopsBefore {
		t.Fatalf("被拒的调用触碰了引擎：before=%d after=%d", loopsBefore, loopsAfter)
	}
	if rounds, loops := runtimeKnobs(service); rounds != roundsBefore || loops != limitBefore {
		t.Fatalf("被拒的调用改了快照：(%q, %d) → (%q, %d)", roundsBefore, limitBefore, rounds, loops)
	}

	close(engine.release[sessionID])
	waitUnitIdle(t, service, sessionID)

	if err := frontendOfTestService(service).SetLoopLimit(ctx, 600); err != nil {
		t.Fatalf("视图空闲后 SetLoopLimit 应当放行：%v", err)
	}
	if _, loops := runtimeKnobs(service); loops != 600 {
		t.Fatalf("放行后 loop limit = %d, want 600", loops)
	}
}

// serviceRuntimePushes 读记录桩里的下发序列（等价于 effortWireRuntime.pushedEfforts，
// 但只吃 *Service，避免测试里四处传桩）。
func serviceRuntimePushes(t *testing.T, service *Service) []string {
	t.Helper()
	runtime, ok := service.Deps.Runtime.(*effortWireRuntime)
	if !ok {
		t.Fatalf("测试桩不对：Deps.Runtime = %T", service.Deps.Runtime)
	}
	return runtime.pushedEfforts()
}
