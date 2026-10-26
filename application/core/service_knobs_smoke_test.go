package core

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// service_knobs_smoke_test.go —— composer 底部两条独立拉条的**全链路冒烟**。
//
// 与 service_knobs_test.go 的分工：那边逐个钉语义（各改一项、范围校验、拒绝不识
// 别的值），这里跑的是**一条完整的链**，也就是 GUI 一次拖动的真实调用序列：
//
//	Bridge → Service.SetXxx → 引擎/账号池落地 → 快照读数 → runtime 投影重建 → 再读
//
// 最容易漏、也最贵的一环是最后那个：runtime 投影每个回合尾/每次工具边界都会**整份
// 重建** Snapshot.Runtime，任何"只写快照、不在投影里反哺"的读数都会在下一次投影时
// 无声清零（拉条自己跳回默认值，后端却还在按旧值跑）。所以冒烟里必须真的调一次
// collectRuntimeProjection + applyRuntimeProjectionLocked。

// knobSmokeEngine 是冒烟用的引擎桩：记下最后一次 SetMaxLoops 的值，并沿用
// multiSessionEngine 的**阻塞** ChatStream（会话 running 才能真的被观察到）。
type knobSmokeEngine struct {
	*multiSessionEngine
	mu       sync.Mutex
	maxLoops int
}

func (engine *knobSmokeEngine) SetMaxLoops(loops int) {
	engine.mu.Lock()
	engine.maxLoops = loops
	engine.mu.Unlock()
}

func (engine *knobSmokeEngine) loops() int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.maxLoops
}

// projectRuntime 模拟一次 runtime 投影重建（回合尾 / 工具边界会做的事）。
func projectRuntime(t *testing.T, service *Service) {
	t.Helper()
	projection := service.collectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	service.ViewMu.Unlock()
}

func TestKnobChainSmoke(t *testing.T) {
	ctx := context.Background()
	engine := &knobSmokeEngine{multiSessionEngine: newMultiSessionEngine()}
	runtime := &effortWireRuntime{fakeRuntime: &fakeRuntime{}}
	service := newTestService(t, engine, withTestRuntime(runtime))

	// 1) 装配期播种：首个快照里的两条读数就是引擎实际生效的默认档（high → high/384）。
	if rounds, loops := runtimeKnobs(service); rounds != dto.ReasoningEffortHigh || loops != 384 {
		t.Fatalf("装配期读数 = (%q, %d), want (high, 384)", rounds, loops)
	}
	if got := engine.loops(); got != 384 {
		t.Fatalf("装配期引擎循环上限 = %d, want 384", got)
	}

	// 2) 拖「循环次数」：引擎与快照一起动。
	if err := frontendOfTestService(service).SetLoopLimit(ctx, 600); err != nil {
		t.Fatalf("SetLoopLimit(600): %v", err)
	}
	if got := engine.loops(); got != 600 {
		t.Fatalf("引擎循环上限 = %d, want 600", got)
	}

	// 3) 拖「推理强度」：账号池拿到 wire 值，循环上限不受影响。
	if err := frontendOfTestService(service).SetReasoningEffort(ctx, dto.ReasoningEffortMax); err != nil {
		t.Fatalf("SetReasoningEffort(max): %v", err)
	}
	if pushed := runtime.pushedEfforts(); len(pushed) != 1 || pushed[0] != dto.ReasoningEffortMax {
		t.Fatalf("下发序列 = %v, want [max]", pushed)
	}
	if _, loops := runtimeKnobs(service); loops != 600 {
		t.Fatalf("拖推理强度把循环上限带走了：%d, want 600", loops)
	}

	// 4) 关键一环：投影整份重建之后两条读数还在（不在投影里反哺就会在这里归零）。
	projectRuntime(t, service)
	rounds, loops := runtimeKnobs(service)
	if rounds != dto.ReasoningEffortMax || loops != 600 {
		t.Fatalf("投影重建后读数 = (%q, %d), want (max, 600)", rounds, loops)
	}
	if got := engine.loops(); got != 600 {
		t.Fatalf("投影重建后引擎循环上限 = %d, want 600", got)
	}

	// 5) 档位是"两条拉条的预设"：切一次 lite，两条一起落到 low/15，思考强度再下发一次。
	if err := frontendOfTestService(service).SwitchEffort(ctx, "lite"); err != nil {
		t.Fatalf("SwitchEffort(lite): %v", err)
	}
	rounds, loops = runtimeKnobs(service)
	if rounds != dto.ReasoningEffortLow || loops != 15 {
		t.Fatalf("切档后读数 = (%q, %d), want (low, 15)", rounds, loops)
	}
	if got := engine.loops(); got != 15 {
		t.Fatalf("切档后引擎循环上限 = %d, want 15", got)
	}
	if pushed := runtime.pushedEfforts(); len(pushed) != 2 || pushed[1] != dto.ReasoningEffortLow {
		t.Fatalf("下发序列 = %v, want [max low]", pushed)
	}

	// 6) 插件切换会重放一次档位（Apply 把引擎循环上限写回档位默认值）——用户的拉条值
	//    不能被这次重放吃掉。这里直接调那条重放（SwitchPlugin 的装配面在别处，见
	//    service_interaction.go 的 reapplyEffortAfterPluginSwitch）。
	if err := frontendOfTestService(service).SetLoopLimit(ctx, 600); err != nil {
		t.Fatalf("SetLoopLimit(600): %v", err)
	}
	service.reapplyEffortAfterPluginSwitch()
	if got := engine.loops(); got != 600 {
		t.Fatalf("档位重放把用户的循环上限吃掉了：%d, want 600", got)
	}
	if _, loops := runtimeKnobs(service); loops != 600 {
		t.Fatalf("档位重放后快照读数 = %d, want 600", loops)
	}

	// 7) 会话运行中：两条都被后端挡住（前端置灰只是体验）。
	if err := service.Submit(ctx, "跑一个回合"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	sessionID := engine.SessionID()
	waitChatStarted(t, engine.started[sessionID])

	// 回合起点按**会话**下发一次强度（账号池是进程级共享的，"每个回合起点按本会话
	// 该用的那条下发一次"是唯一能保证后台/定时会话不继承别的会话强度的位置）。
	if pushed := runtime.pushedEfforts(); len(pushed) != 3 || pushed[2] != dto.ReasoningEffortLow {
		t.Fatalf("回合起点没有按会话下发强度：%v, want [max low low]", pushed)
	}

	if err := frontendOfTestService(service).SetLoopLimit(ctx, 999); err == nil {
		t.Fatal("运行中 SetLoopLimit 必须被拒")
	}
	if err := frontendOfTestService(service).SetReasoningEffort(ctx, dto.ReasoningEffortLow); err == nil {
		t.Fatal("运行中 SetReasoningEffort 必须被拒")
	}
	if got := engine.loops(); got != 600 {
		t.Fatalf("被拒的调用碰了引擎：%d, want 600", got)
	}
	if pushed := runtime.pushedEfforts(); len(pushed) != 3 {
		t.Fatalf("被拒的调用下发了值：%v", pushed)
	}

	close(engine.release[sessionID])
	waitUnitIdle(t, service, sessionID)
}
