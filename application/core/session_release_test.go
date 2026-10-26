package core

// 会话释放面用例（session_release.go）：受保护集不释放 + 窗口（受保护集之外保留
// 几个）+ 释放**完整**（引擎实例 / 桥侧会话本体 / 可见正文一起放掉）。
//
// 口径见 docs/2026-10-14-memory-cpu-accumulation/release-surface.md：
//   - 受保护集 = 当前视图 ∪ 待审批 ∪ 运行·排队·在飞 ∪ 正在恢复；
//   - 窗口 = limits.session_release_window（默认 1 ⇒ 只保留上一个）；
//   - 释放 = 引擎 + 桥侧本体 + 正文；会话单元/目录行/磁盘事实保留。

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSessionReleaseApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionReleaseApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionReleaseApplication")
	}
	if field.Type != reflect.TypeOf((*applicationsession.ReleaseApplication)(nil)) {
		t.Fatalf("sessionReleaseApplication 字段类型 = %s", field.Type)
	}
}

func TestSessionReleaseFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_release.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"releaseSessionMemory":    false,
		"reconcileSessionRelease": false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Service") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 sessionReleaseApplication 转发", function.Name.Name, len(function.Body.List))
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("未找到 %s", name)
		}
	}
}

// withSessionReleaseWindow 把统一窗口改成 window，并把两个分层覆盖键归零
// （0 = 交给统一窗口管辖），返回还原函数（与 withResidentLimit / withContentLimit
// 同法，core 顺序用例内调用）。
func withSessionReleaseWindow(window int) func() {
	previous := Limits()
	applied := previous
	applied.SessionReleaseWindow = window
	applied.ResidentSessionLimit = 0
	applied.LoadedContentLimit = 0
	ApplyLimits(applied)
	return func() { ApplyLimits(previous) }
}

// releaseAwareEngine 在 multiSessionEngine 之上记录「桥侧会话本体」的释放
// （application 侧 ReleaseSessionBundle 端口；生产实现是
// assemble.Runtime.ReleaseSessionBundle）。没有这一格，用例只能看到"引擎被
// 卸载了"，而看不出"驱逐是否真的把本体放掉了"——那正是 2026-10-14 的缺口。
type releaseAwareEngine struct {
	*multiSessionEngine
	mu       sync.Mutex
	released []string
}

func newReleaseAwareEngine() *releaseAwareEngine {
	return &releaseAwareEngine{multiSessionEngine: newMultiSessionEngine()}
}

// ReleaseSessionBundle 模拟桥侧释放面：记录被释放的会话并回报"真的释放了"。
func (engine *releaseAwareEngine) ReleaseSessionBundle(sessionID string) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.released = append(engine.released, sessionID)
	return true
}

func (engine *releaseAwareEngine) wasReleased(sessionID string) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for _, id := range engine.released {
		if id == sessionID {
			return true
		}
	}
	return false
}

// contentReleaseEngine 是内容层用例的引擎桩：**没有**会话引擎面（HasSession 不
// 满足 residentEngine ⇒ 驻留层不参与），只有桥侧释放面，用来单独验内容层的窗口
// 收敛（正文层自己该丢掉上一个之外的正文）。
type contentReleaseEngine struct {
	*fakeEngine
	mu       sync.Mutex
	released []string
}

func (engine *contentReleaseEngine) ReleaseSessionBundle(sessionID string) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.released = append(engine.released, sessionID)
	return true
}

// waitEngineReleased 轮询等待引擎侧会话离场（驱逐在 touch 内同步完成；-race/高负载
// 下给一个极短收敛窗，只容忍环境抖动，不改断言语义）。
func waitEngineReleased(t *testing.T, engine *releaseAwareEngine, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for engine.HasSession(sessionID) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// goalBundleExists 报告 goal 协调器是否还持有该会话的运行时句柄（内含它按会话
// 缓存的那份上下文存储适配器）。
func goalBundleExists(service *Service, sessionID string) bool {
	goal := service.components.goal
	goal.mu.Lock()
	defer goal.mu.Unlock()
	return goal.sessions[sessionID] != nil
}

// TestPointViewAtRestoringShellKeepsTargetUnitContent：切回"装载在途"的会话时，视图
// 必须归位到目标，且**不得清空目标会话单元**——在途装载可能已经把回读出来的内容写进
// 单元了，清空等于把用户内容抹掉（窗口=1 的释放面下"切回已释放会话"是常见路径）。
func TestPointViewAtRestoringShellKeepsTargetUnitContent(t *testing.T) {
	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-view"}, {ID: "sess-target"}})

	engine.register("sess-view")
	if err := service.ResumeSession("sess-view"); err != nil {
		t.Fatalf("resume sess-view: %v", err)
	}

	// 目标会话：装载在途（restoring），单元里已经有在途装载写进去的内容。
	service.ViewMu.Lock()
	service.setRestoringLocked("sess-target")
	service.sessionUnitLocked("sess-target")
	service.ViewMu.Unlock()
	service.components.view.SessionViewMutateLocked("sess-target", func(view *session.View) {
		view.Conversation = []model.Message{{Role: "assistant", Content: "loaded"}}
	})

	service.pointViewAtRestoringShell("sess-target")

	snapshot := service.Snapshot()
	if snapshot.Session.ID != "sess-target" {
		t.Fatalf("视图未归位：%q，want sess-target", snapshot.Session.ID)
	}
	if snapshot.Session.Status != SessionStatusRestoring {
		t.Fatalf("状态 = %q，want restoring（装载仍在途）", snapshot.Session.Status)
	}
	if state := readContentState(t, service, "sess-target"); state.window != 1 || state.contents[0] != "loaded" {
		t.Fatalf("目标会话单元被清空/改写：%#v", state)
	}
}

// TestPointViewAtRestoringShellIgnoresSettledTarget：装载已收口（restoring 已清）时
// 不得冒充一次激活——那会把已发布的内容换成空壳；视图归位交给正常切换路径。
func TestPointViewAtRestoringShellIgnoresSettledTarget(t *testing.T) {
	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-view"}, {ID: "sess-settled"}})

	engine.register("sess-view")
	if err := service.ResumeSession("sess-view"); err != nil {
		t.Fatalf("resume sess-view: %v", err)
	}

	service.pointViewAtRestoringShell("sess-settled")

	if snapshot := service.Snapshot(); snapshot.Session.ID != "sess-view" {
		t.Fatalf("视图被改成 %q，want sess-view（restoring 已清，不该动视图）", snapshot.Session.ID)
	}
}

// TestReleaseSessionMemoryForgetsGoalStoreHandle：释放面必须通知"按会话缓存了
// 上下文存储句柄"的下游一起清格（goal 协调器的第五栈持久化适配器）。不放掉，
// 重开会话会绑新的 store，而旧句柄还钉在适配器里——同一个会话两个 store 同时
// 写盘（存储层单写者纪律失效）。
func TestReleaseSessionMemoryForgetsGoalStoreHandle(t *testing.T) {
	restore := withSessionReleaseWindow(1)
	defer restore()

	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-gone"}, {ID: "sess-view"}})

	for _, sessionID := range []string{"sess-gone", "sess-view"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	// 目标会话先建起 goal 运行时（模拟"曾经开过 goal 看板"）。
	service.components.goal.bundleFor("sess-gone")
	if !goalBundleExists(service, "sess-gone") {
		t.Fatal("夹具前提失败：goal 运行时未建起")
	}

	if err := service.releaseSessionMemory("sess-gone", releaseIdle); err != nil {
		t.Fatalf("release idle: %v", err)
	}
	if goalBundleExists(service, "sess-gone") {
		t.Fatal("释放后 goal 运行时句柄仍在（重开会绑新 store，旧句柄留着 = 两个写者）")
	}
}

// TestSessionReleaseWindowKeepsOnlyPreviousSession：窗口 1 的口径——三个会话依次
// 激活，最旧的那个被**完整释放**（引擎 + 桥侧本体），上一个与当前视图都保留。
func TestSessionReleaseWindowKeepsOnlyPreviousSession(t *testing.T) {
	restore := withSessionReleaseWindow(1)
	defer restore()

	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-1"}, {ID: "sess-2"}, {ID: "sess-3"}})

	for _, sessionID := range []string{"sess-1", "sess-2", "sess-3"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	waitEngineReleased(t, engine, "sess-1")
	if engine.HasSession("sess-1") {
		t.Fatalf("sess-1 引擎仍驻留（窗口 1 应释放最旧的）\n%s", engine.debugSnapshot("sess-1", "sess-2", "sess-3"))
	}
	if !engine.wasReleased("sess-1") {
		t.Fatal("sess-1 的桥侧会话本体未被释放：驱逐必须走完整释放面，只丢引擎引用等于空操作")
	}
	if !engine.HasSession("sess-2") || !engine.HasSession("sess-3") {
		t.Fatalf("上一个（sess-2）或当前视图（sess-3）被误释放\n%s", engine.debugSnapshot("sess-2", "sess-3"))
	}
	if engine.wasReleased("sess-2") || engine.wasReleased("sess-3") {
		t.Fatal("窗口 1 只该释放最旧的一个：sess-2/sess-3 不该被释放")
	}
	if residentOf(t, service, "sess-1") {
		t.Fatal("sess-1 驻留标记未随释放清除")
	}
	if !residentOf(t, service, "sess-2") || !residentOf(t, service, "sess-3") {
		t.Fatal("sess-2/sess-3 的驻留标记丢失")
	}
}

// TestSessionReleaseWindowProtectsBusySession：运行中会话是受保护集，即使它是最旧
// 的驻留会话也不释放；窗口按"受保护集之外"计数（留着最近的那一个空闲会话）。
func TestSessionReleaseWindowProtectsBusySession(t *testing.T) {
	restore := withSessionReleaseWindow(1)
	defer restore()

	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()

	if err := service.Submit(t.Context(), "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	// 空闲 B/C/D 依次热激活：窗口 1 ⇒ 受保护集（运行中的 A + 当前视图 D）之外
	// 只留最近的一个（C），最旧的空闲 B 被释放。
	sessions.setInfos([]model.SessionInfo{{ID: aID}, {ID: "sess-b"}, {ID: "sess-c"}, {ID: "sess-d"}})
	for _, sessionID := range []string{"sess-b", "sess-c", "sess-d"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	if !engine.HasSession(aID) {
		t.Fatalf("运行中的 A 被释放（受保护集必须永不释放）\n%s", engine.debugSnapshot(aID))
	}
	if engine.HasSession("sess-b") {
		t.Fatalf("窗口 1：最旧的空闲会话 sess-b 应被释放\n%s", engine.debugSnapshot("sess-b", "sess-c", "sess-d"))
	}
	if !engine.HasSession("sess-c") {
		t.Fatalf("窗口 1 应保留最近的一个空闲会话（sess-c）\n%s", engine.debugSnapshot("sess-c"))
	}
	if !engine.HasSession("sess-d") {
		t.Fatal("当前视图会话被释放")
	}
	close(engine.release[aID])
	if err := service.WaitForIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestSessionReleaseWindowDropsPreviousContent：窗口 1 下最旧会话的**可见正文**
// 也随释放离场（只丢内存副本：窗口标志与磁盘消息不动），上一个会话的正文保留。
//
// 这里用没有会话引擎面的桩（HasSession 不满足 ⇒ 驻留层不参与），验的是**内容层
// 自身的窗口收敛**：受保护集之外只留最近的一个。
func TestSessionReleaseWindowDropsPreviousContent(t *testing.T) {
	restore := withSessionReleaseWindow(1)
	defer restore()
	restoreWindow := withHistoryWindow(4)
	defer restoreWindow()

	store := newMultiPagedStore(map[string]int{"sess-a": 10, "sess-b": 10, "sess-c": 10})
	service := newTestService(t, &contentReleaseEngine{fakeEngine: &fakeEngine{}}, withTestSessions(store))
	defer service.Shutdown()

	for _, sessionID := range []string{"sess-a", "sess-b", "sess-c"} {
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	evicted := assertContentLoaded(t, service, "sess-a", false)
	if !evicted.unloaded {
		t.Fatalf("sess-a 正文未被卸载：%#v", evicted)
	}
	// 窗口标志保留（冷回读据此还原同一窗口位置），磁盘消息数不变（不丢数据）。
	if evicted.total != 10 || evicted.offset != 6 {
		t.Fatalf("sess-a 窗口标志 = total %d offset %d, want total 10 offset 6", evicted.total, evicted.offset)
	}
	if got := store.messageCount("sess-a"); got != 10 {
		t.Fatalf("sess-a 磁盘消息数 = %d, want 10（卸载不得丢数据）", got)
	}
	assertContentWindow(t, service, "sess-b", 6, 4)
	assertContentWindow(t, service, "sess-c", 6, 4)
}

// TestDeleteSessionReleasesMemory：删除**非视图**会话必须把内存一起放掉（引擎 +
// 桥侧本体 + 会话单元），不只是删存储记录。缺这一步，"删掉的会话"会一直留到进程重启。
func TestDeleteSessionReleasesMemory(t *testing.T) {
	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-gone"}, {ID: "sess-view"}})

	// 先激活要删的会话，再切到 sess-view：删除时 sess-gone 是**非视图**会话。
	for _, sessionID := range []string{"sess-gone", "sess-view"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	if err := service.DeleteSession("sess-gone"); err != nil {
		t.Fatalf("delete sess-gone: %v", err)
	}
	if engine.HasSession("sess-gone") {
		t.Fatal("删除后引擎实例仍在内存")
	}
	if !engine.wasReleased("sess-gone") {
		t.Fatal("删除未释放桥侧会话本体（框架 Session / 上下文存储）")
	}
	if unit := service.sessions.Unit("sess-gone"); unit != nil {
		t.Fatal("删除后会话单元仍在内存")
	}
}

// TestDeleteViewSessionSwitchesViewToFreshDraft：删除**当前视图**会话时，重的三面
// （引擎 / 桥侧本体 / 正文）与会话单元、标题一起释放——但视图单例必须始终有对应单元，
// 所以删除后视图要**就地切到一个新草稿**（"删掉正在看的那个会话"是前端的正常操作）。
func TestDeleteViewSessionSwitchesViewToFreshDraft(t *testing.T) {
	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-view"}})

	engine.register("sess-view")
	if err := service.ResumeSession("sess-view"); err != nil {
		t.Fatalf("resume sess-view: %v", err)
	}
	if err := service.DeleteSession("sess-view"); err != nil {
		t.Fatalf("delete sess-view: %v", err)
	}
	if engine.HasSession("sess-view") {
		t.Fatal("删除当前视图会话后引擎实例仍在内存")
	}
	if !engine.wasReleased("sess-view") {
		t.Fatal("删除当前视图会话未释放桥侧会话本体")
	}
	if unit := service.sessions.Unit("sess-view"); unit != nil {
		t.Fatal("删除后会话单元仍在内存")
	}
	viewID := service.Snapshot().Session.ID
	if viewID == "sess-view" {
		t.Fatalf("视图仍指着已删除的会话：%q", viewID)
	}
	if unit := service.sessions.Unit(viewID); unit == nil {
		t.Fatalf("视图单例 %q 失去对应单元", viewID)
	}
}

// TestReleaseSessionMemorySkipsProtectedSession：释放面自身带二次确认——直接调用
// 释放当前视图会话是空操作（受保护集永不释放；卸载入口另有 releaseFull 口径）。
func TestReleaseSessionMemorySkipsProtectedSession(t *testing.T) {
	restore := withSessionReleaseWindow(1)
	defer restore()

	engine := newReleaseAwareEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]model.SessionInfo{{ID: "sess-view"}})

	engine.register("sess-view")
	if err := service.ResumeSession("sess-view"); err != nil {
		t.Fatalf("resume sess-view: %v", err)
	}
	if err := service.releaseSessionMemory("sess-view", releaseIdle); err != nil {
		t.Fatalf("release idle: %v", err)
	}
	if !engine.HasSession("sess-view") {
		t.Fatal("当前视图会话被 releaseIdle 释放：受保护集必须挡住")
	}
	if engine.wasReleased("sess-view") {
		t.Fatal("当前视图会话的桥侧本体被 releaseIdle 释放")
	}
}
