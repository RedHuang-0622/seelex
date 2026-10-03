package core

// 触发场景全覆盖的**存活断言**（把 2026-09-29 那条守卫从显式路径扩到全部入口）。
//
// 历史：装配层压缩的落点（prepareExecutionContextFor）原先把「推进状态 + 推帧 +
// 帧正文落存储」放在**单个** Core.ViewMu 写锁临界区里，而生产推帧会回调宿主读面
// （main.go 注入的 CompressedTurnArchiver.SessionIDProvider 读 app.Snapshot()），
// 于是同 goroutine 持写锁再取读锁 = 确定性自锁（f643f2d 修，见
// docs/devlog/2026-09-29-compaction-fold-lock-granularity.md）。
//
// 那次的守卫只有两条，都落在**显式**路径上：
//   - context_compact_selfdeadlock_repro_test.go（/compact + 生产归档接线）
//   - context_compact_viewmu_hold_repro_test.go（/compact + 推帧可阻塞的索引面桩）
//
// 但压缩的入口不止一条（自动硬阈值 / 维护入口 / 无在飞回合的会话级 / 显式与自动
// 并发）。本文件把**同一条判据面**铺到这些入口上：推帧进行中，交互面
// （ViewMu 写锁本身 / Snapshot / 提交入口 / 切会话）必须照常返回。
//
// 判据与"修法是否还在"一一对应：
//   - 任一入口把推帧放回 ViewMu 临界区 → 本文件对应用例立刻红（写锁拿不到）；
//   - 推帧桩内部读一次视图快照，与生产归档器同形：若某条入口把 pushLock 与 ViewMu
//     按相反次序取（持 ViewMu 等 pushLock，而推帧持 pushLock 等 ViewMu.RLock），
//     这里就是死锁的观测点。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/internal/testutil"
)

// reentrantGateIndexRuntime 是"生产形状"的索引面桩：
//
//   - 推帧内部先读一次视图快照（= main.go 的 CompressedTurnArchiver.SessionIDProvider
//     → app.Snapshot() → ViewMu.RLock）；
//   - 第一次推帧阻塞到 release（= 前缀重放厚摘要的模型调用 / 原文归档写盘），
//     其余推帧直接放行（同会话链式栈由 compactionPushLock 串行）。
type reentrantGateIndexRuntime struct {
	runtimeWithContextLimits
	entered  chan struct{}
	release  chan struct{}
	first    sync.Once
	snapshot func()
}

func (runtime *reentrantGateIndexRuntime) PushCompactionFrame(
	_ context.Context,
	sessionID string,
	_ context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	if runtime.snapshot != nil {
		runtime.snapshot()
	}
	blocked := false
	runtime.first.Do(func() { blocked = true })
	if blocked {
		close(runtime.entered)
		<-runtime.release
	}
	return context_runtime.CompactionIndexReceipt{
		SegmentID:     "seg-trigger-" + sessionID,
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n触发场景存活断言",
		SummarySource: "local",
	}, nil
}

// triggerLivenessFixture 是一个"已越过硬阈值、必折出非空溢出区"的会话：
// 4 个已定稿轮（每轮约 4 万 tokens）足以让自动路径与显式路径都真的走到推帧。
func triggerLivenessFixture(t *testing.T, requestID string) (*Service, *reentrantGateIndexRuntime, string) {
	t.Helper()
	runtime := &reentrantGateIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{
			fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	runtime.snapshot = func() { _ = service.Snapshot() }
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID}
	service.components.tasks.BeginTask(requestID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, requestID)
	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		sessionID = service.components.tasks.SessionIDForRequest(requestID)
	}
	return service, runtime, sessionID
}

// releaseGate 造一个**幂等**放行器：返回的函数可以反复调用（含 defer + 显式调用），
// 只关一次通道。每条用例都必须拿到它并挂 defer，否则 t.Cleanup 的 Shutdown 会跟着
// 被冻住。
func releaseGate(runtime *reentrantGateIndexRuntime) func() {
	var once sync.Once
	return func() { once.Do(func() { close(runtime.release) }) }
}

// awaitPushEntered 等到夹具真的走到推帧（否则判据是空集上的真命题）。
func awaitPushEntered[T any](t *testing.T, runtime *reentrantGateIndexRuntime, inFlight <-chan T, what string) {
	t.Helper()
	select {
	case <-runtime.entered:
	case <-inFlight:
		t.Fatalf("%s 在进入推帧之前就返回了，夹具没走到判定点", what)
	case <-time.After(5 * time.Second):
		t.Fatalf("5s 内没有进入推帧（夹具失效）：%s", what)
	}
}

// assertInteractionFaceLive 断言交互面四个入口在推帧进行中照常返回。任一被冻 =
// 这条入口把慢活放进了临界区（或与 pushLock 互等）。
func assertInteractionFaceLive(t *testing.T, service *Service, what string) {
	t.Helper()
	if !returnsWithin(time.Second, func() {
		service.ViewMu.Lock()
		service.ViewMu.Unlock()
	}) {
		t.Fatalf("%s：推帧期间 ViewMu 写锁拿不到（推帧落在临界区内 = 自锁/互等）", what)
	}
	if !returnsWithin(time.Second, func() { _ = service.Snapshot() }) {
		t.Fatalf("%s：推帧期间快照取不到（会话切不动、列表与右栏不刷新）", what)
	}
	if !returnsWithin(time.Second, func() { _ = service.Submit(context.Background(), "hello") }) {
		t.Fatalf("%s：推帧期间提交入口不返回（消息连队列都进不去）", what)
	}
	if !returnsWithin(time.Second, func() { _ = service.resumeSession("session-other") }) {
		t.Fatalf("%s：推帧期间切会话不返回", what)
	}
}

// TestAutoCompactionPushKeepsInteractionFaceLive：**自动**入口（rawTokens ≥ 硬阈值，
// 无显式压缩门）推帧进行中，交互面必须照常。这条入口是真实进程里最常发生的压缩。
func TestAutoCompactionPushKeepsInteractionFaceLive(t *testing.T) {
	const requestID = "task-trigger-auto"
	service, runtime, sessionID := triggerLivenessFixture(t, requestID)
	release := releaseGate(runtime)
	defer release()

	assembled := make(chan error, 1)
	go func() {
		_, err := service.components.context.PrepareExecutionContextFor(sessionID, requestID, "")
		assembled <- err
	}()

	awaitPushEntered(t, runtime, assembled, "自动装配压缩")
	assertInteractionFaceLive(t, service, "自动压缩")

	release()
	select {
	case err := <-assembled:
		if err != nil {
			t.Fatalf("释放推帧后自动装配失败：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("释放推帧后自动装配仍未返回")
	}
}

// TestMaintenanceCompactionPushKeepsInteractionFaceLive：**维护入口**
// （CompactTaskContextFor，引擎迭代 hook / 控制器驱动的压缩）推帧进行中，
// 交互面必须照常——它是唯一一条"调用目的本身就是折出 checkpoint"的入口。
func TestMaintenanceCompactionPushKeepsInteractionFaceLive(t *testing.T) {
	const requestID = "task-trigger-maintenance"
	service, runtime, sessionID := triggerLivenessFixture(t, requestID)
	release := releaseGate(runtime)
	defer release()

	compacted := make(chan error, 1)
	go func() { compacted <- service.components.context.CompactTaskContextFor(sessionID, requestID) }()

	awaitPushEntered(t, runtime, compacted, "维护入口压缩")
	assertInteractionFaceLive(t, service, "维护入口压缩")

	release()
	select {
	case err := <-compacted:
		if err != nil {
			t.Fatalf("释放推帧后维护压缩失败：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("释放推帧后维护压缩仍未返回")
	}
}

// TestSessionLevelCompactPushKeepsInteractionFaceLive：**无在飞回合的会话级**
// 入口（冷加载 / 刚清空上的 `/compact`，走 compactSessionContextWithoutEpoch +
// 会话级维护身份）推帧进行中，交互面必须照常。
//
// 这条入口额外多两处 ViewMu 进出（开/撤维护身份），因此它是"锁纪律"最容易破的
// 一条：把推帧挪进任一临界区，写锁判据立刻红。
func TestSessionLevelCompactPushKeepsInteractionFaceLive(t *testing.T) {
	runtime := &reentrantGateIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{
			fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	runtime.snapshot = func() { _ = service.Snapshot() }
	// 刻意**不** BeginTask：会话没有在飞回合，但有已装载的对话材料。
	appendIndexRounds(t, service, "task-trigger-cold")
	release := releaseGate(runtime)
	defer release()

	commandReturned := make(chan struct{})
	go func() {
		_ = service.Submit(context.Background(), "/compact")
		close(commandReturned)
	}()

	awaitPushEntered(t, runtime, commandReturned, "会话级 /compact")
	assertInteractionFaceLive(t, service, "会话级 /compact")

	release()
	select {
	case <-commandReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("释放推帧后 /compact 仍未返回")
	}
}

// TestExplicitCompactionWhileAutoCompactionInFlightDoesNotInterlock：**显式与自动并发**。
//
// 自动压缩（回合开始前的装配，不经显式压缩门）与显式 `/compact` 可以同时在同一
// 会话上压缩；两者共用一条按会话键的推帧串行锁（compactionPushLock）。本用例把
// 那个交叉点变成判定点：
//
//	自动压缩：持 pushLock → 读视图快照（ViewMu.RLock）→ 阻塞在推帧里；
//	显式压缩：持压缩门 → 走到推帧 → 等 pushLock。
//
// 显式这一侧**不得**在持 ViewMu 的情况下等 pushLock（否则与"推帧持 pushLock 读
// 快照"构成 AB-BA）。判据就是交互面的写锁判据：能不能拿到 ViewMu 写锁。
func TestExplicitCompactionWhileAutoCompactionInFlightDoesNotInterlock(t *testing.T) {
	const requestID = "task-trigger-both"
	service, runtime, sessionID := triggerLivenessFixture(t, requestID)
	release := releaseGate(runtime)
	defer release()

	autoDone := make(chan error, 1)
	go func() {
		_, err := service.components.context.PrepareExecutionContextFor(sessionID, requestID, "")
		autoDone <- err
	}()
	awaitPushEntered(t, runtime, autoDone, "自动装配压缩（并发夹具）")

	commandDone := make(chan struct{})
	go func() {
		_ = service.Submit(context.Background(), "/compact")
		close(commandDone)
	}()
	// 让显式这一侧走到推帧排队（或自己收口）；无论哪种，交互面都必须活着。
	time.Sleep(200 * time.Millisecond)
	assertInteractionFaceLive(t, service, "显式与自动并发压缩")

	release()
	select {
	case <-commandDone:
	case <-time.After(10 * time.Second):
		t.Fatal("释放推帧后 /compact 仍未返回（显式与自动互等）")
	}
	select {
	case err := <-autoDone:
		if err != nil {
			t.Fatalf("释放推帧后自动装配失败：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("释放推帧后自动装配仍未返回")
	}
}

// TestCompactionChurnOnOneSessionDoesNotHang：把触发入口混在一起反复跑（显式压缩
// 门 + 自动装配 + 交互面读 + 切会话），每一步都有界。判据不是"最终态对不对"，而是
// **没有任何一步卡住**——非重入锁上的互等会在这里恒定为真地挂在某一轮上。
func TestCompactionChurnOnOneSessionDoesNotHang(t *testing.T) {
	const requestID = "task-trigger-churn"
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID}
	service.components.tasks.BeginTask(requestID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, requestID)
	sessionID := service.Snapshot().Session.ID

	const iterations = 6
	step := func(what string, fn func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- fn() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s：%v", what, err)
			}
		// 6 个并发迭代 × 整条压缩链路，是这一族存活断言里最重的一条：
		// `-race` 下把预算放宽（testutil.Budget 在 race 构建里 ×4），
		// 否则量到的是插桩开销而不是"挂没挂住"。
		case <-time.After(testutil.Budget(15 * time.Second)):
			t.Fatalf("%s 在 15s 内没返回（压缩链路某一步挂住了）", what)
		}
	}

	var wait sync.WaitGroup
	for iteration := 0; iteration < iterations; iteration++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			step("显式 /compact", func() error { return service.Submit(context.Background(), "/compact") })
		}()
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := service.components.context.PrepareExecutionContextFor(sessionID, requestID, ""); err != nil {
				t.Errorf("自动装配：%v", err)
			}
		}()
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = service.Snapshot()
			_ = service.resumeSession("session-other")
		}()
	}
	// 并发收敛：任一步永久挂住都会让 wait 永不返回，这里用有界等待把它变成失败。
	settled := make(chan struct{})
	go func() { wait.Wait(); close(settled) }()
	select {
	case <-settled:
	case <-time.After(60 * time.Second):
		t.Fatal("混合触发入口并发负载没有收敛（有步骤永久挂住）")
	}
	_ = sessionID
}
