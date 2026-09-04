package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// switchDuringInFlightChat 在引擎回合仍在执行（未 release）时切换视图到另
// 一个空闲会话，测量切换是否等待引擎。引擎忙窗口同时覆盖“工具调用中/LLM
// 输出中”两类真实场景（工具/流的具体顺序语义由
// TestToolOrderingStableAfterSwitchToRunningSession、
// TestDeltasKeepFlowingAfterSwitchToRunningSession 另行覆盖）。
func switchDuringInFlightChat(t *testing.T, label string) {
	t.Helper()
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "task in flight"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	engine.register("sess-b")
	sessions.setInfos([]SessionInfo{{ID: aID}, {ID: "sess-b"}})

	start := time.Now()
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("[%s] switch while in-flight: %v", label, err)
	}
	switchCost := time.Since(start)
	if switchCost > 2*time.Second {
		t.Fatalf("[%s] switch took %s while engine in-flight（疑似等待引擎锁）", label, switchCost)
	}
	if got := service.Snapshot().Session.ID; got != "sess-b" {
		t.Fatalf("[%s] active session after switch = %q, want sess-b", label, got)
	}
	// 原会话仍驻留且运行中（未被切换打断）。
	if !engine.HasSession(aID) {
		t.Fatalf("[%s] source session engine lost after switch", label)
	}
	close(engine.release[aID])
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("[%s] switch while in-flight cost %s（引擎回合未被打断）", label, switchCost)
}

// TestSwitchDuringToolInvocation 复现：工具调用（引擎忙）期间切换会话。
func TestSwitchDuringToolInvocation(t *testing.T) {
	switchDuringInFlightChat(t, "tool-invocation")
}

// TestSwitchDuringLLMStreaming 复现：LLM 输出（引擎忙）期间切换会话。
func TestSwitchDuringLLMStreaming(t *testing.T) {
	switchDuringInFlightChat(t, "llm-streaming")
}

// gatedHistorySessions 在 LoadHistory 上设门闩，用于复现“会话冷加载中再切
// 换到别的会话”的串行等待路径。
type gatedHistorySessions struct {
	*scopedSessions
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	once        sync.Once
	armMu       sync.Mutex
	armed       bool
}

func (sessions *gatedHistorySessions) releaseNow() {
	sessions.releaseOnce.Do(func() { close(sessions.release) })
}

func (sessions *gatedHistorySessions) LoadHistory(sessionID string) ([]EngineMessage, error) {
	if !sessions.gateEnabled(sessionID) {
		return sessions.scopedSessions.LoadHistory(sessionID)
	}
	return sessions.scopedSessions.LoadHistory(sessionID)
}

func (sessions *gatedHistorySessions) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	if !sessions.gateEnabled(sessionID) {
		return sessions.scopedSessions.LoadHistoryRange(sessionID, offset, limit)
	}
	return sessions.scopedSessions.LoadHistoryRange(sessionID, offset, limit)
}

func (sessions *gatedHistorySessions) gateEnabled(sessionID string) bool {
	sessions.armMu.Lock()
	armed := sessions.armed
	sessions.armMu.Unlock()
	if !armed || sessionID != "sess-cold" {
		return false
	}
	sessions.once.Do(func() { close(sessions.entered) })
	<-sessions.release
	return true
}

// TestSwitchDuringColdLoadSerializes 复现：会话 A 处于冷加载（历史装载被
// 门闩阻塞）时，切到 B 会等待 A 完成——当前视图过渡仍按视图 key 串行。
// 用例用于度量该等待并防止将来出现死锁（等待应随 A 释放而收敛）。
func TestSwitchDuringColdLoadSerializes(t *testing.T) {
	engine := newMultiSessionEngine()
	now := time.Now()
	gated := &gatedHistorySessions{
		scopedSessions: &scopedSessions{
			catalog: map[string][]SessionInfo{
				"": {
					{ID: "sess-cold", UpdatedAt: now},
					{ID: "sess-b", UpdatedAt: now},
				},
			},
			histories: map[string]map[string][]EngineMessage{
				"": {
					"sess-cold": {{Role: "user", Content: "cold content"}},
					"sess-b":    {{Role: "user", Content: "b content"}},
				},
			},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestService(t, engine, withTestSessions(gated))
	defer service.Shutdown()
	t.Cleanup(gated.releaseNow)

	engine.register("sess-b")
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("initial resume b: %v", err)
	}
	gated.armMu.Lock()
	gated.armed = true
	gated.armMu.Unlock()

	coldDone := make(chan error, 1)
	go func() {
		coldDone <- service.ResumeSession("sess-cold")
	}()
	select {
	case <-gated.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("cold load never entered history read")
	}

	switchDone := make(chan error, 1)
	go func() {
		switchDone <- service.ResumeSession("sess-b")
	}()
	select {
	case err := <-switchDone:
		t.Fatalf("switch to b returned before cold load released: %v", err)
	case <-time.After(200 * time.Millisecond):
		// 预期：视图 key 串行，B 等待 A 冷加载完成——记录该等待为瓶颈证据。
	}
	gated.releaseNow()

	select {
	case err := <-coldDone:
		if err != nil {
			t.Fatalf("cold resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cold resume deadlocked after release")
	}
	select {
	case err := <-switchDone:
		if err != nil {
			t.Fatalf("switch to b after cold load: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("switch to b deadlocked after cold load released")
	}
	t.Log("cold-load switch: b waits until a finishes cold load（视图 key 串行瓶颈，已收敛无死锁）")
}

// TestBeginNewSessionSingleDraftOwner 复现/钉住“新建会话”的幂等与责任链：
// 存在当前草稿时重复 BeginNewSession 不产生新 ID；列表里同时出现多个草稿
// 行时只展示当前草稿（旧草稿隐藏、不删除数据）。
func TestBeginNewSessionSingleDraftOwner(t *testing.T) {
	now := time.Now()
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{
			"": {
				{ID: "draft-old", Status: SessionStatusDraft, UpdatedAt: now.Add(-time.Hour)},
				{ID: "draft-current", Status: SessionStatusDraft, UpdatedAt: now},
			},
		},
		histories: map[string]map[string][]EngineMessage{
			"": {"draft-old": nil, "draft-current": nil},
		},
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.draft = &draftSlot{ID: "draft-current", CreatedAt: now, UpdatedAt: now}
	service.Core.Snapshot.Session = SessionState{ID: "draft-current", Draft: true, Status: SessionStatusDraft}
	service.sessions.SetActive("draft-current")
	service.ViewMu.Unlock()

	first := service.Snapshot().Session.ID
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Session.ID; got != first || got != "draft-current" {
		t.Fatalf("BeginNewSession changed draft owner: before=%q after=%q", first, got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle: %v", err)
	}
	snapshot := service.Snapshot()
	rows := sessionIDsOf(snapshot.Sessions)
	if containsSessionID(rows, "draft-old") {
		t.Fatalf("stale draft visible in list: %v", rows)
	}
	if !containsSessionID(rows, "draft-current") {
		t.Fatalf("current draft missing from list: %v", rows)
	}
	listed := service.ListSessions()
	if containsSessionID(sessionIDsOf(listed), "draft-old") {
		t.Fatalf("stale draft visible in ListSessions: %v", listed)
	}
}
