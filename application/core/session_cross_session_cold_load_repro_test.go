package core

// 复现：**一次会话冷加载会打到另一个会话身上**（用户报告：「一个会话激发了
// 会话的冷加载，然后另一个会话就容易断掉」）。
//
// 两条判据（各自的机理由源码给出，断言只认可观测量）：
//
//  1. 冷加载在**进程级视图过渡 key** 上读完整个磁盘才放手。
//     生产宿主 seelebridge.Runtime.PerSessionExecution()==false
//     （seelebridge/runtime.go:718），于是 service.transitionForSession
//     （application/core/session_scope.go:37）一律回退 transitionForKey("")
//     ——视图过渡锁是**全进程唯一**的一把。resumeSession
//     （application/core/session_history.go:30）在持有这把锁期间调用
//     resumeSessionCold（:246），后者在锁内做三读（record/history/transcript，
//     :270 起）、wire 装配、引擎恢复、队列回填。于是另一个会话的
//     SubmitToSession → ActivateSession（session_scope.go 的 SubmitToSession
//     未加载分支）在这把锁上排队，整段冷加载期间消息发不出去（GUI 侧表现为
//     "另一个会话没反应/超时报错"）。
//
//  2. 后台冷加载即使**不激活视图**（mayActivate=false，视图已切回运行中的
//     另一个会话），仍然无条件改写**进程级**执行面：Deps.Runtime.
//     SwitchSessionTasks（把实时 task 注册表的 currentTaskSessionID 换成冷
//     会话，见 seelebridge/ports.go:277）与 Deps.Runtime.ClearSubagentTree
//     （清空全进程子代理树，session_history.go:404-405）。运行中会话的
//     执行面因此被冷加载抢走。
//
// 两条用例都以"稳定红"的形式报告：先证明当前代码的问题，并附上阻塞现场。

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// gatedColdSessions 只在冷会话 "sess-cold" 的区间读上设门闩：冷加载一旦进入
// 磁盘读就通知测试，并阻塞到 release —— 把"冷加载消费的时间"变成可控判定点。
type gatedColdSessions struct {
	*scopedSessions
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	once        sync.Once
}

func (sessions *gatedColdSessions) releaseNow() {
	sessions.releaseOnce.Do(func() { close(sessions.release) })
}

func (sessions *gatedColdSessions) LoadHistory(sessionID string) ([]EngineMessage, error) {
	history, err := sessions.scopedSessions.LoadHistory(sessionID)
	sessions.gate(sessionID)
	return history, err
}

func (sessions *gatedColdSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	rows, total, err := sessions.scopedSessions.LoadHistoryRange(sessionID, offset, limit)
	sessions.gate(sessionID)
	return rows, total, err
}

func (sessions *gatedColdSessions) gate(sessionID string) {
	if sessionID != "sess-cold" {
		return
	}
	sessions.once.Do(func() { close(sessions.entered) })
	<-sessions.release
}

func newGatedColdSessions() *gatedColdSessions {
	now := time.Now()
	return &gatedColdSessions{
		scopedSessions: &scopedSessions{
			catalog: map[string][]SessionInfo{
				"": {
					{ID: "sess-cold", UpdatedAt: now},
					{ID: "sess-b", UpdatedAt: now},
				},
			},
			histories: map[string]map[string][]EngineMessage{
				"": {
					"sess-cold": {
						{Role: "user", Content: "cold-1"},
						{Role: "assistant", Content: "cold-2"},
					},
					"sess-b": {
						{Role: "user", Content: "b-1"},
						{Role: "assistant", Content: "b-2"},
					},
				},
			},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// scopeSpyRuntime 记录进程级执行面的改写调用（SwitchSessionTasks /
// ClearSubagentTree），即在位 fakeRuntime 的所有时序行为。
type scopeSpyRuntime struct {
	*fakeRuntime
	mu           sync.Mutex
	currentTask  string
	switchCalls  []string
	clearTreeHit int
}

func (runtime *scopeSpyRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	runtime.mu.Lock()
	runtime.currentTask = sessionID
	runtime.switchCalls = append(runtime.switchCalls, sessionID)
	runtime.mu.Unlock()
	runtime.fakeRuntime.SwitchSessionTasks(sessionID, records)
}

func (runtime *scopeSpyRuntime) ClearSubagentTree() error {
	runtime.mu.Lock()
	runtime.clearTreeHit++
	runtime.mu.Unlock()
	return runtime.fakeRuntime.ClearSubagentTree()
}

func (runtime *scopeSpyRuntime) snapshot() (string, []string, int) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.currentTask, append([]string(nil), runtime.switchCalls...), runtime.clearTreeHit
}

// callerStacks 抓取全部 goroutine 栈（阻塞现场证据）。
func callerStacks() string {
	buffer := make([]byte, 1<<20)
	size := runtime.Stack(buffer, true)
	return string(buffer[:size])
}

// transitionLockFrames 从栈里挑出与过渡锁/冷加载相关的帧（截断输出用）。
func transitionLockFrames(stacks string) string {
	var kept []string
	for _, block := range strings.Split(stacks, "\n\n") {
		if strings.Contains(block, "transition") || strings.Contains(block, "resumeSession") ||
			strings.Contains(block, "ActivateSession") || strings.Contains(block, "SubmitToSession") {
			kept = append(kept, block)
		}
	}
	return strings.Join(kept, "\n----\n")
}

// waitRestoringCleared 等目标会话的后台冷加载走完（restoring 清除）。冷加载
// 的 commit 段里，进程级执行面改写发生在 clearRestoringLocked 之前。
func waitRestoringCleared(t *testing.T, service *Service, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		service.ViewMu.RLock()
		restoring := service.isRestoringLocked(sessionID)
		service.ViewMu.RUnlock()
		if !restoring {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("会话 %s 的后台冷加载未在 5s 内收口", sessionID)
}

// releaseEngineSession 释放测试引擎的会话回合门闩（幂等；会话从未运行过时
// 是空操作）——用例红态也要能把夹具收干净。
func releaseEngineSession(engine *multiSessionEngine, sessionID string) {
	engine.mu.Lock()
	release := engine.release[sessionID]
	engine.mu.Unlock()
	if release == nil {
		return
	}
	select {
	case <-release:
	default:
		close(release)
	}
}

// drainColdLoadFixture 释放冷加载门闩与引擎门闸，把现场收干净后在调用方报告
// 失败（红绿两态都必须能退出：红态下断言失败不许把测试留在阻塞里）。
func drainColdLoadFixture(t *testing.T, service *Service, engine *multiSessionEngine, sessions *gatedColdSessions) {
	t.Helper()
	sessions.releaseNow()
	releaseEngineSession(engine, "sess-b")
	idleCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.WaitForIdle(idleCtx); err != nil {
		t.Logf("收尾时 WaitForIdle: %v", err)
	}
}

// TestReproColdLoadBlocksAnotherSessionSubmit：A 冷加载（历史装载中）时，
// 另一个会话 B 的提交必须照常受理——今天的代码会把 B 排在 A 的磁盘读之后。
func TestReproColdLoadBlocksAnotherSessionSubmit(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := newGatedColdSessions()
	service := newTestService(t, engine, withTestSessions(sessions))
	defer sessions.releaseNow()

	coldDone := make(chan error, 1)
	go func() { coldDone <- service.ResumeSession("sess-cold") }()

	select {
	case <-sessions.entered:
	case err := <-coldDone:
		t.Fatalf("冷加载在读盘之前就返回了（夹具没走到要复现的那一步）：%v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("3s 内没有进入冷加载的磁盘读（夹具失效）")
	}

	// B 未加载 → SubmitToSession 走 ActivateSession（resumeSession）→ 需要
	// 同一把进程级视图过渡 key；A 的冷加载此刻正持有它。
	submitDone := make(chan error, 1)
	go func() { submitDone <- service.SubmitToSession(context.Background(), "sess-b", "hello from b") }()

	var submitErr error
	blocked := true
	var lateStacks string
	select {
	case submitErr = <-submitDone:
		blocked = false
	case <-time.After(800 * time.Millisecond):
		lateStacks = transitionLockFrames(callerStacks())
	}

	// 先收现场（放门闩 / 放引擎 / 等空闲），再报告结论：红态也要能干净退出。
	sessions.releaseNow()
	select {
	case err := <-coldDone:
		if err != nil {
			t.Fatalf("冷加载: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("释放门闩后冷加载未收口")
	}
	if blocked {
		// 红态：提交仍排在冷加载的过渡锁上，等它被受理再收尾。
		select {
		case err := <-submitDone:
			if submitErr == nil {
				submitErr = err
			}
		case <-time.After(5 * time.Second):
		}
	}
	if !blocked && submitErr != nil {
		t.Fatalf("B 的提交返回错误：%v", submitErr)
	}
	releaseEngineSession(engine, "sess-b")

	if blocked {
		t.Fatalf("红灯：A 冷加载在读盘期间攥着**进程级**视图过渡锁（seelebridge.Runtime.PerSessionExecution()==false → "+
			"session_scope.go:37 回退 transitionForKey(\"\")），B 会话的提交（SubmitToSession→ActivateSession）整段排队，"+
			"800ms 内没有受理——GUI 侧就是「另一个会话发不出消息/超时报错」。\n阻塞现场（相关 goroutine）：\n%s",
			lateStacks)
	}
}

// TestReproColdLoadStealsSharedRuntimeScopeFromRunningSession：B 在飞（引擎
// 阻塞）期间，A 的后台冷加载不得改写进程级执行面（实时 task 注册表的归属
// 会话 / 子代理树）。
func TestReproColdLoadStealsSharedRuntimeScopeFromRunningSession(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := newGatedColdSessions()
	spy := &scopeSpyRuntime{fakeRuntime: &fakeRuntime{}}
	service := newTestService(t, engine, withTestSessions(sessions), withTestRuntime(spy))
	defer sessions.releaseNow()
	ctx := context.Background()

	// B：驻留 + 在飞（ChatStream 阻塞在 release）。
	engine.register("sess-b")
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume b: %v", err)
	}
	if err := service.Submit(ctx, "long B"); err != nil {
		t.Fatalf("submit b: %v", err)
	}
	select {
	case <-engine.started["sess-b"]:
	case <-time.After(3 * time.Second):
		t.Fatal("B 的回合没有开始（夹具失效）")
	}

	// A：未驻留 + 有会话运行中 → restoring 空壳 + 后台冷加载。
	if err := service.ResumeSession("sess-cold"); err != nil {
		t.Fatalf("resume cold: %v", err)
	}
	select {
	case <-sessions.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("3s 内没有进入 A 的冷加载磁盘读（夹具失效）")
	}

	// 视图在 A 装载完成前切回运行中的 B：A 的装载至此 mayActivate=false，
	// 只允许完成它自己会话的状态，不得抢占进程级执行面。
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("switch back to b: %v", err)
	}
	sessions.releaseNow()
	waitRestoringCleared(t, service, "sess-cold")

	current, switches, clearTreeHit := spy.snapshot()
	if current != "sess-b" || clearTreeHit != 0 {
		drainColdLoadFixture(t, service, engine, sessions)
		t.Fatalf("红灯：A 的后台冷加载（视图已切回运行中的 B，mayActivate=false）抢走了进程级执行面："+
			"实时 task 注册表归属会话=%q（want \"sess-b\"）、ClearSubagentTree 调用=%d 次（want 0）；"+
			"调用序列=%v。\nsession_history.go:404-405 的 SwitchSessionTasks/ClearSubagentTree 在锁内无条件执行，"+
			"seelebridge/ports.go:277 会把 currentTaskSessionID 换成冷会话、并把 B 的实时注册表写进分区快照；"+
			"ClearSubagentTree 清空的是**全进程**子代理树（GUI「清空」按钮的同一入口）。",
			current, clearTreeHit, switches)
	}
	drainColdLoadFixture(t, service, engine, sessions)
}
