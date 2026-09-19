package core

// 复现（用户报告）：一个会话运行中时，另一个**没在运行**的会话的输入框内容提交
// 被它带偏。两条机制各自独立，都由前端把普通输入显式钉给视图会话
// （gui/frontend/dist/composer-input.js `composerSubmitPlan` → `SubmitToSession`）
// 之后暴露：
//
//	① 目标会话是尚未物化的草稿（新会话/重启恢复的草稿）：SubmitToSession 只认
//	   "引擎 bundle 在不在"，草稿按"未加载的冷会话"去冷回读，materializeDraftSession
//	   整条路径（引擎按 SID 建束、项目绑定、标题、草稿清空）全部被跳过。
//	② 视图停在空闲会话上点"新建会话"：BeginNewSession 用进程级活跃别名读写引擎
//	   （Engine.History()/ClearHistory()），而别名可能正指向另一个运行中的会话
//	   ——Seele framework Session 的锁被 ChatStream 从进函数持到出函数。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// startedFor 加锁取某会话的"回合已进入"通道（started 映射由 ChatStreamFor
// 在锁内按需写入，裸读是数据竞争）。
func (engine *multiSessionEngine) startedFor(sessionID string) <-chan struct{} {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.started[sessionID]
}

// waitStreamCall 轮询直到目标会话的引擎被调用（延后/后台启动都要等它）。
func waitStreamCall(t *testing.T, engine *multiSessionEngine, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for engine.streamCallsFor(sessionID) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if engine.streamCallsFor(sessionID) == 0 {
		t.Fatalf("session %s never entered the engine", sessionID)
	}
}

// draftRoutedFixture 是「生产形状」的会话路由宿主：路由引擎 + 支持 record 读写的
// 会话端口（composer 草稿落盘与物化清空都经它）。
func draftRoutedFixture(t *testing.T) (*multiSessionEngine, *draftRecordStore, *Service) {
	t.Helper()
	engine := newMultiSessionEngine()
	store := newDraftRecordStore()
	return engine, store, newTestService(t, engine, withTestSessions(store))
}

// assertDraftSubmitMaterialized 锁定"显式提交到草稿"必须等于物化：
// 新会话正文里不得出现冷加载标记、草稿槽必须消费、草稿 record 必须清空。
func assertDraftSubmitMaterialized(t *testing.T, service *Service, store *draftRecordStore, draftID string) {
	t.Helper()
	snapshot := service.Snapshot()
	for index, message := range snapshot.Conversation {
		if strings.HasPrefix(message.Content, "已恢复会话") {
			t.Fatalf("草稿被当成冷会话回读（conv[%d]=%q）；首条提交必须走物化：%+v", index, message.Content, snapshot.Conversation)
		}
	}
	if snapshot.Session.Draft {
		t.Fatalf("提交后视图仍是草稿：%+v", snapshot.Session)
	}
	service.ViewMu.RLock()
	slot := service.draft
	service.ViewMu.RUnlock()
	if slot != nil {
		t.Fatalf("草稿槽位未被物化消费：%+v", slot)
	}
	store.mu.Lock()
	record := store.records[draftID]
	store.mu.Unlock()
	if record.Status == SessionStatusDraft || record.Composer.Text != "" {
		t.Fatalf("幽灵草稿残留（重启后已发送的正文会回到输入框）：%+v", record)
	}
}

// TestSubmitToSessionMaterializesDraftWhileOtherSessionRuns 是用户报告的原始场景：
// A 运行中，在"新会话"的输入框里写了话并回车。
//
// RED（当前实现）：A 运行中 ⇒ 草稿未驻留 ⇒ SubmitToSession 先 ActivateSession(草稿)
// → resumeSession 走**异步冷加载**分支（判据是"存在任一会话运行中"）→ 视图变成
// restoring 空壳、提交被挂到装载完成点；后台装载的是那条 draft record，于是新会话
// 以「已恢复会话: draft_…」开头，草稿槽位与草稿 record 都没被物化路径清理。
// 无会话运行时同一提交走同步冷加载，症状少一个 restoring 窗口，其余相同。
func TestSubmitToSessionMaterializesDraftWhileOtherSessionRuns(t *testing.T) {
	engine, store, service := draftRoutedFixture(t)
	ctx := context.Background()

	// ① A 运行中（引擎回合保持未释放）。
	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.startedFor(aID))

	// ② 运行中新建会话并在输入框里留下正文（已按草稿落盘）。
	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if draftID == "" || !service.Snapshot().Session.Draft {
		t.Fatalf("视图不是早分配草稿：%+v", service.Snapshot().Session)
	}
	if err := service.SaveComposerDraft("新会话第一条"); err != nil {
		t.Fatalf("SaveComposerDraft: %v", err)
	}

	// ③ 前端把这条输入显式钉给草稿会话（不是 ambient Submit）。
	if err := service.SubmitToSession(ctx, draftID, "新会话第一条"); err != nil {
		t.Fatalf("SubmitToSession(draft) = %v, want nil", err)
	}
	waitStreamCall(t, engine, draftID)
	engine.releaseSession(draftID)
	engine.releaseSession(aID)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}

	assertDraftSubmitMaterialized(t, service, store, draftID)

	// ④ 输入归属：草稿会话收到了自己的话，A 没有被塞进别人的输入。
	draftView, err := service.SnapshotOf(draftID)
	if err != nil {
		t.Fatalf("SnapshotOf(draft): %v", err)
	}
	if !containsUserText(draftView.Conversation, "新会话第一条") {
		t.Fatalf("草稿会话视图缺少本次输入：%+v", draftView.Conversation)
	}
	aView, err := service.SnapshotOf(aID)
	if err != nil {
		t.Fatalf("SnapshotOf(A): %v", err)
	}
	if containsUserText(aView.Conversation, "新会话第一条") {
		t.Fatalf("运行中会话 A 吸走了草稿的输入：%+v", aView.Conversation)
	}
}

// TestSubmitToSessionMaterializesIdleDraft 是同一条判据的对照组：没有会话运行中
// 时，草稿的显式提交同样必须物化（这条路径不经 restoring，纯粹证明 ① 不是
// restoring 的副产物）。
func TestSubmitToSessionMaterializesIdleDraft(t *testing.T) {
	engine, store, service := draftRoutedFixture(t)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.startedFor(aID))
	engine.releaseSession(aID)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if err := service.SaveComposerDraft("空闲草稿第一条"); err != nil {
		t.Fatalf("SaveComposerDraft: %v", err)
	}
	if err := service.SubmitToSession(ctx, draftID, "空闲草稿第一条"); err != nil {
		t.Fatalf("SubmitToSession(draft) = %v, want nil", err)
	}
	waitStreamCall(t, engine, draftID)
	engine.releaseSession(draftID)
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle after draft submit: %v", err)
	}
	assertDraftSubmitMaterialized(t, service, store, draftID)
}

// aliasBusyEngine 模拟 Seele framework Session 的锁纪律：ChatStream 从进函数持锁
// 到出函数（一次长文流式可达数十秒），期间同一会话上的 History()/ClearHistory()
// 一律排队。生产里"同一会话"由**进程级活跃别名**决定，因此别名指向谁，谁被冻住。
type aliasBusyEngine struct {
	*multiSessionEngine
	mu           sync.Mutex
	cond         *sync.Cond
	busy         map[string]bool
	historyCalls int
	clearCalls   int
}

func newAliasBusyEngine() *aliasBusyEngine {
	engine := &aliasBusyEngine{
		multiSessionEngine: newMultiSessionEngine(),
		busy:               map[string]bool{},
	}
	engine.cond = sync.NewCond(&engine.mu)
	return engine
}

func (engine *aliasBusyEngine) setBusy(sessionID string, value bool) {
	engine.mu.Lock()
	if value {
		engine.busy[sessionID] = true
	} else {
		delete(engine.busy, sessionID)
		engine.cond.Broadcast()
	}
	engine.mu.Unlock()
}

// waitAliasFree 阻塞直到"活跃别名那一会话"的回合结束（= 真锁语义）。
func (engine *aliasBusyEngine) waitAliasFree() {
	alias := engine.multiSessionEngine.SessionID()
	engine.mu.Lock()
	for engine.busy[alias] {
		engine.cond.Wait()
	}
	engine.mu.Unlock()
}

func (engine *aliasBusyEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.setBusy(sessionID, true)
	defer engine.setBusy(sessionID, false)
	return engine.multiSessionEngine.ChatStreamFor(sessionID, ctx, input, onChunk)
}

func (engine *aliasBusyEngine) History() []EngineMessage {
	engine.mu.Lock()
	engine.historyCalls++
	engine.mu.Unlock()
	engine.waitAliasFree()
	return engine.multiSessionEngine.History()
}

func (engine *aliasBusyEngine) ClearHistory() {
	engine.mu.Lock()
	engine.clearCalls++
	engine.mu.Unlock()
	engine.waitAliasFree()
	engine.multiSessionEngine.ClearHistory()
}

func (engine *aliasBusyEngine) aliasCalls() (int, int) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.historyCalls, engine.clearCalls
}

// TestBeginNewSessionDoesNotSerializeBehindRunningSession 是 ② 的复现：
// A 在后台跑，用户看的却是空闲会话 B，此时点"新建会话"。
//
// RED（当前实现）：BeginNewSession 在持有视图过渡锁（生产宿主 PerSessionExecution
// = false ⇒ 那是**进程唯一**的 key，所有会话的 resume/Unload 与 ambient Submit 都要
// 抢它）的情况下读 `Engine.History()` 并 `Engine.ClearHistory()`——别名指向 A，
// 于是这一次点击排在 A 那一轮之后，用户视角"界面冻住、输入发不出去"；解除阻塞后
// 被清空的还是 A 的工作历史。
func TestBeginNewSessionDoesNotSerializeBehindRunningSession(t *testing.T) {
	engine := newAliasBusyEngine()
	store := newDraftRecordStore()
	service := newTestService(t, engine, withTestSessions(store))
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.startedFor(aID))

	// 视图移到空闲会话 B（热挂载只换视图指针，别名仍指向运行中的 A）。
	engine.register("sess-b")
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("ResumeSession(B): %v", err)
	}
	if got := service.Snapshot().Session.ID; got != "sess-b" {
		t.Fatalf("view = %s, want sess-b", got)
	}

	done := make(chan error, 1)
	go func() { done <- service.BeginNewSession() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("BeginNewSession: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		historyCalls, clearCalls := engine.aliasCalls()
		engine.releaseSession(aID)
		<-done
		t.Fatalf("BeginNewSession 排在运行中会话的引擎别名后面（History 调用 %d 次 / ClearHistory %d 次）；空闲会话的输入准备被运行中会话挡住",
			historyCalls, clearCalls)
	}

	engine.releaseSession(aID)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}
	// 运行中会话的工作历史不属于"被切走的视图会话"：新建草稿不得清空它。
	if len(engine.HistoryFor(aID)) == 0 {
		t.Fatalf("运行中会话 %s 的引擎历史被新建草稿清掉", aID)
	}
}

// TestIdleSessionSubmitWhileOtherRunningLandsInViewSession 是用户报告场景的**正面
// 判据**（前两条机制修好之后的"这条链路现在长什么样"）：A 运行中，用户切到一个
// **没在运行**的会话 B，在 B 的输入框里提交。
//
// 两条提交入口都必须落在 B：
//   - 显式钉会话（渲染层普通输入走 `SubmitToSession(视图会话 ID, text)`）；
//   - ambient `Submit`（sigil 输入交回后端输入路由器，按"当前视图会话"分派）。
//
// 断言同时覆盖四个面：引擎调用归属、A 的会话队列、视图快照的会话级运行态、可见
// 会话归属。任何一面把 B 的输入接到 A（"运行中会话吸走别人的输入"）都判定失败。
func TestIdleSessionSubmitWhileOtherRunningLandsInViewSession(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	// ① A 运行中（引擎回合保持未释放）。
	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.startedFor(aID))

	// ② 视图切到空闲会话 B（已驻留 → 热挂载，只换视图指针）。
	engine.register("sess-b")
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("ResumeSession(B): %v", err)
	}
	view := service.Snapshot()
	if view.Session.ID != "sess-b" || view.Session.Status != SessionStatusIdle {
		t.Fatalf("view = %s/%s, want sess-b/idle", view.Session.ID, view.Session.Status)
	}
	if view.Chat.Running {
		t.Fatalf("空闲视图的运行态被后台运行中的 %s 带偏：%+v", aID, view.Chat)
	}

	// ③ 显式钉会话提交（普通输入）→ 落在 B。
	if err := service.SubmitToSession(ctx, "sess-b", "msg for idle B"); err != nil {
		t.Fatalf("SubmitToSession(B): %v", err)
	}
	waitStreamCall(t, engine, "sess-b")

	// ④ ambient 提交（sigil 路径）→ 同样落在 B（B 运行中，进的是 B 自己的队列）。
	if err := service.Submit(ctx, "又一句给 B"); err != nil {
		t.Fatalf("Submit(ambient): %v", err)
	}
	engine.releaseSession("sess-b")
	// B 的两轮都跑完（第二句是从 B 自己的队列里消费的），A 仍在后台跑。
	waitSessionChatIdle(t, service, "sess-b")

	// 归属：A 的引擎/队列/可见会话都不得出现 B 的输入。
	if calls := engine.streamCallsFor(aID); calls != 1 {
		t.Fatalf("A streamCalls = %d, want 1（只跑自己的那一轮）", calls)
	}
	for _, message := range engine.HistoryFor(aID) {
		if strings.Contains(message.Content, "idle B") || strings.Contains(message.Content, "又一句给 B") {
			t.Fatalf("运行中会话 A 的引擎历史吸走了发给 B 的输入：%+v", engine.HistoryFor(aID))
		}
	}
	service.ViewMu.RLock()
	queuedA := len(service.sessions.Unit(aID).PendingRequests())
	service.ViewMu.RUnlock()
	if queuedA != 0 {
		t.Fatalf("运行中会话 A 的队列吸走了发给 B 的输入：queued=%d", queuedA)
	}
	bView, err := service.SnapshotOf("sess-b")
	if err != nil {
		t.Fatalf("SnapshotOf(B): %v", err)
	}
	if !containsUserText(bView.Conversation, "msg for idle B") || !containsUserText(bView.Conversation, "又一句给 B") {
		t.Fatalf("空闲会话 B 的可见会话缺少本次输入：%+v", bView.Conversation)
	}
	aView, err := service.SnapshotOf(aID)
	if err != nil {
		t.Fatalf("SnapshotOf(A): %v", err)
	}
	if containsUserText(aView.Conversation, "msg for idle B") {
		t.Fatalf("运行中会话 A 吸走了发给空闲会话的输入：%+v", aView.Conversation)
	}

	engine.releaseSession(aID)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle A: %v", err)
	}
}

// waitSessionChatIdle 轮询到指定会话自己的回合跑完（不看别的会话——后台会话
// 仍在跑时 WaitForIdle 会一直等）。
func waitSessionChatIdle(t *testing.T, service *Service, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		service.ViewMu.RLock()
		unit := service.sessions.Unit(sessionID)
		running := unit != nil && unit.ChatState().Running
		service.ViewMu.RUnlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("会话 %s 的回合没有收尾", sessionID)
}

func containsUserText(messages []Message, text string) bool {
	for _, message := range messages {
		if message.Role == "user" && strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}
