package core

// 阶段 2 生命周期用例（test-cases.md 第 4/8 节）：
// TC-A3-01/02 运行中会话允许 hot_attach 回看（不触碰执行）；
// TC-LC-02 hot_attach 不重放历史；
// TC-LC-03 unload 释放 scope/引擎/任务运行时，重开走 cold_load。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

// TestResumeRunningSessionAllowsHotAttach（TC-A3-01 新语义）：运行中会话
// resume 允许回看——换视图指针，不触碰执行态。
func TestResumeRunningSessionAllowsHotAttach(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("session A did not start:\n%s", dumpAllGoroutines())
	}
	historyBefore := engine.HistoryFor(aID)

	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("ResumeSession(running A) = %v, want hot attach nil", err)
	}
	service.ViewMu.RLock()
	running := service.sessions.Unit(aID).ChatState().Running
	active := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if active != aID {
		t.Fatalf("active = %q, want %q", active, aID)
	}
	if !running {
		t.Fatal("A stopped running after hot attach")
	}
	historyAfter := engine.HistoryFor(aID)
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("hot attach replayed/rebuilt history: before=%d after=%d",
			len(historyBefore), len(historyAfter))
	}

	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestHotAttachDoesNotTouchRunningSession（TC-A3-02）：B 活跃、A 后台运行中
// resume A → A 的运行态与引擎历史逐字节不变。
func TestHotAttachDoesNotTouchRunningSession(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("session A did not start:\n%s", dumpAllGoroutines())
	}
	bID := "sess-hot-B"
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("resume B while A running: %v", err)
	}
	historyBefore := engine.HistoryFor(aID)

	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("hot attach A while running: %v", err)
	}
	service.ViewMu.RLock()
	runningA := service.sessions.Unit(aID).ChatState().Running
	service.ViewMu.RUnlock()
	if !runningA {
		t.Fatal("A stopped running after hot attach")
	}
	historyAfter := engine.HistoryFor(aID)
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("hot attach mutated A engine history: before=%d after=%d",
			len(historyBefore), len(historyAfter))
	}

	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestHotAttachNoReplay（TC-LC-02）：空闲驻留会话 resume 只换指针，不重建
// 引擎历史（无重放）。
func TestHotAttachNoReplay(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("session A did not start:\n%s", dumpAllGoroutines())
	}
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	bID := "sess-noreplay-B"
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatal(err)
	}
	historyBefore := engine.HistoryFor(aID)

	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("hot attach A: %v", err)
	}
	historyAfter := engine.HistoryFor(aID)
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("hot attach replayed history: before=%d after=%d",
			len(historyBefore), len(historyAfter))
	}
	if snap := service.Snapshot(); snap.Session.ID != aID {
		t.Fatalf("active = %q, want %q", snap.Session.ID, aID)
	}
}

// TestUnloadReleasesScope（TC-LC-03）：unload 释放会话内存态（chat 运行态、
// 视图 scope、任务运行时、引擎实例）；重开走 cold_load 恢复。
func TestUnloadReleasesScope(t *testing.T) {
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: session_runtime.SessionRecordVersion,
			ID:      "sess-unload",
			Title:   SessionTitle{Value: "Unload me", Source: "user"},
			Conversation: ConversationRecord{Messages: []Message{
				{ID: "u-1", Role: "user", Content: "persisted content"},
			}},
		},
		transcript: []TranscriptEvent{
			{Seq: 1, TaskID: "req-1", Role: "user", Content: "persisted content", TokenCount: 4},
		},
	}
	engine := newMultiSessionEngine()
	service := newTestService(t, engine, withTestSessions(sessions))
	const sessionID = "sess-unload"

	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatal(err)
	}
	if !engine.HasSession(sessionID) {
		t.Fatalf("session %q not loaded after cold load", sessionID)
	}
	if err := service.UnloadSession(sessionID); err != nil {
		t.Fatalf("unload: %v", err)
	}
	service.ViewMu.RLock()
	hasUnit := service.sessions.Unit(sessionID) != nil
	service.ViewMu.RUnlock()
	if hasUnit {
		t.Fatalf("unload retained session unit: %q", sessionID)
	}
	if engine.HasSession(sessionID) {
		t.Fatal("unload retained engine instance")
	}
	// 重开走 cold_load：从持久化恢复。
	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatalf("reopen after unload: %v", err)
	}
	if !containsMessageContent(service.Snapshot().Conversation, "persisted content") {
		t.Fatalf("reopened conversation lost persisted content: %v",
			conversationTextsForTest(service.Snapshot().Conversation))
	}
}

// TestUnloadRejectsRunningSession（阶段 D · 卸载/提交互斥）：unload 与 submit
// 共用同一把会话切换锁，"读 running → 释放内存态"整段对提交原子 —— 运行中
// 必须拒绝卸载，回合收尾后才允许，且单元确实不再驻留。
func TestUnloadRejectsRunningSession(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	service := newTestService(t, engine)
	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not start")
	}
	sessionID := service.Snapshot().Session.ID
	if err := service.UnloadSession(sessionID); !errors.Is(err, ErrChatRunning) {
		close(engine.release)
		t.Fatalf("unload while running = %v, want ErrChatRunning", err)
	}
	close(engine.release)
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.UnloadSession(sessionID); err != nil {
		t.Fatalf("unload after idle: %v", err)
	}
	service.ViewMu.RLock()
	hasUnit := service.sessions.Unit(sessionID) != nil
	service.ViewMu.RUnlock()
	if hasUnit {
		t.Fatalf("unload retained session unit %q", sessionID)
	}
}

func containsMessageContent(messages []Message, needle string) bool {
	for _, message := range messages {
		if message.Content == needle {
			return true
		}
	}
	return false
}

func conversationTextsForTest(conversation []Message) []string {
	texts := make([]string, 0, len(conversation))
	for _, message := range conversation {
		texts = append(texts, message.Role+":"+message.Content)
	}
	return texts
}
