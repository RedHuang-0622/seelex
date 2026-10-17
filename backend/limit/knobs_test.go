package limit

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// fakeKnobsHost 是 `KnobsHost` 的结构满足实现：把"宿主锁内的调用"记下来，
// 以便断言加锁次序（组件写读数必须发生在宿主锁内）与发布语义。
type fakeKnobsHost struct {
	viewID     string
	viewRun    bool
	effort     string
	maxLoops   map[string]int
	sink       contract.ReasoningEffortPort
	engineSets []int
	mutations  int
	publishes  int
	lastPushed string
}

func (host *fakeKnobsHost) ViewSession() (string, bool) { return host.viewID, host.viewRun }

func (host *fakeKnobsHost) MutateRuntime(mutate func(*model.RuntimeState)) {
	host.mutations++
	if mutate == nil {
		return
	}
	mutate(&model.RuntimeState{})
}

func (host *fakeKnobsHost) PublishRuntime(mutate func(*model.RuntimeState)) error {
	host.publishes++
	if mutate == nil {
		return nil
	}
	mutate(&model.RuntimeState{})
	return nil
}

func (host *fakeKnobsHost) SetEngineMaxLoops(limit int) {
	host.engineSets = append(host.engineSets, limit)
}

func (host *fakeKnobsHost) CurrentEffort() string { return host.effort }

func (host *fakeKnobsHost) MaxLoopsForEffort(level string) int { return host.maxLoops[level] }

func (host *fakeKnobsHost) ReasoningEffortSink() contract.ReasoningEffortPort { return host.sink }

// fakeEffortSink 是 contract.ReasoningEffortPort 的结构满足实现。
type fakeEffortSink struct {
	applied []string
	changed int
}

func (sink *fakeEffortSink) SetSessionReasoningEffort(wireValue string) int {
	sink.applied = append(sink.applied, wireValue)
	return sink.changed
}

func newFakeKnobsHost() *fakeKnobsHost {
	return &fakeKnobsHost{viewID: "view-1", effort: "high", maxLoops: map[string]int{"high": 384, "low": 15}}
}

// TestKnobsEnsureViewIdleReturnsViewSessionID：守卫返回视图会话 ID；running 时给拒绝理由。
func TestKnobsEnsureViewIdleReturnsViewSessionID(t *testing.T) {
	host := newFakeKnobsHost()
	knobs := NewKnobs(host)

	if id, err := knobs.EnsureViewIdle("调整循环次数"); err != nil || id != "view-1" {
		t.Fatalf("idle 时应放行并返回视图会话 ID，got %q, %v", id, err)
	}
	host.viewRun = true
	id, err := knobs.EnsureViewIdle("调整循环次数")
	if err == nil || !strings.Contains(err.Error(), "当前会话正在运行") ||
		!strings.Contains(err.Error(), "调整循环次数") {
		t.Fatalf("running 时应带动作名的拒绝理由，got %q, %v", id, err)
	}
	if id != "view-1" {
		t.Fatalf("拒绝时也要返回视图会话 ID（调用方要用它写目标会话），got %q", id)
	}
}

// TestKnobsCurrentLoopLimitFallsBackToEffortPreset：拉条没改过 → 回退档位预设；
// 改过 → 用改过的值（"拧到 600 后一次退栈不该无声回到 384"）。
func TestKnobsCurrentLoopLimitFallsBackToEffortPreset(t *testing.T) {
	host := newFakeKnobsHost()
	knobs := NewKnobs(host)

	if got := knobs.CurrentLoopLimit(); got != 384 {
		t.Fatalf("未改过时应回退当前档位预设 384，got %d", got)
	}
	knobs.ApplyLoopLimit(600)
	if got := knobs.CurrentLoopLimit(); got != 600 {
		t.Fatalf("改过之后应生效 600，got %d", got)
	}
	// 档位切走也不能悄悄覆盖用户拧过的值。
	host.effort = "low"
	if got := knobs.CurrentLoopLimit(); got != 600 {
		t.Fatalf("用户拧过的值不该被档位覆盖，got %d", got)
	}
}

// TestKnobsApplyLoopLimitWritesInsideHostLock：引擎与两条读数一次改完（避免半截状态）。
func TestKnobsApplyLoopLimitWritesInsideHostLock(t *testing.T) {
	host := newFakeKnobsHost()
	knobs := NewKnobs(host)

	knobs.ApplyLoopLimit(600)
	if len(host.engineSets) != 1 || host.engineSets[0] != 600 {
		t.Fatalf("引擎应先收到新上限，got %v", host.engineSets)
	}
	if host.mutations != 1 {
		t.Fatalf("读数必须经宿主锁内写面（MutateRuntime），got %d 次", host.mutations)
	}
	if _, got := knobs.Readings(); got != 600 {
		t.Fatalf("宿主锁内应同时写组件读数，got %d", got)
	}
	// 非法值（<=0）不是"设成 0"，而是 no-op。
	knobs.ApplyLoopLimit(0)
	if len(host.engineSets) != 1 {
		t.Fatalf("<=0 应 no-op，got %v", host.engineSets)
	}
}

// TestKnobsSetReasoningEffortPushesThenPublishes：先下发强度、再记读数、最后发一次
// 完整 runtime.changed。
func TestKnobsSetReasoningEffortPushesThenPublishes(t *testing.T) {
	host := newFakeKnobsHost()
	sink := &fakeEffortSink{changed: 2}
	host.sink = sink
	knobs := NewKnobs(host)

	if err := knobs.SetReasoningEffort(" low "); err != nil {
		t.Fatal(err)
	}
	if len(sink.applied) != 1 || sink.applied[0] != "low" {
		t.Fatalf("应先按归一化后的 wire 值下发，got %v", sink.applied)
	}
	if effort, _ := knobs.Readings(); effort != "low" {
		t.Fatalf("读数应更新为 low，got %q", effort)
	}
	if host.publishes != 1 {
		t.Fatalf("应发布一次 runtime.changed，got %d", host.publishes)
	}
}

// TestKnobsPushWithoutSinkIsNoOp：宿主没装配强度端口时整步退化为记录读数，不报错。
func TestKnobsPushWithoutSinkIsNoOp(t *testing.T) {
	host := newFakeKnobsHost()
	knobs := NewKnobs(host)

	if got := knobs.PushSessionReasoningEffort("low"); got != 0 {
		t.Fatalf("无端口时应返回 0，got %d", got)
	}
	if err := knobs.SetReasoningEffort("low"); err != nil {
		t.Fatalf("无端口不算失败：%v", err)
	}
	if effort, _ := knobs.Readings(); effort != "low" {
		t.Fatalf("读数仍要更新，got %q", effort)
	}
}

// TestKnobsBorrowTakeIsOneShot：按会话租一笔强度，取走即消掉；空参数是 no-op。
func TestKnobsBorrowTakeIsOneShot(t *testing.T) {
	knobs := NewKnobs(newFakeKnobsHost())

	knobs.BorrowSessionReasoningEffort(" ", "low")
	knobs.BorrowSessionReasoningEffort("s1", " ")
	if _, ok := knobs.TakeSessionReasoningEffort("s1"); ok {
		t.Fatal("空参数不该记下租约")
	}
	knobs.BorrowSessionReasoningEffort(" s1 ", " low ")
	if wire, ok := knobs.TakeSessionReasoningEffort("s1"); !ok || wire != "low" {
		t.Fatalf("取走应得到归一化后的值，got %q/%t", wire, ok)
	}
	if _, ok := knobs.TakeSessionReasoningEffort("s1"); ok {
		t.Fatal("取走即消掉：第二次必须没有")
	}
}
