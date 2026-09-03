package core

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestLongRunningSessionSurvivesViewSwitch（视图切换隔离用例）：先开启一个
// 长会话 A（引擎阻塞、运行中），经视图切换（ResumeSession）切到 B；B 正常
// 完成一轮。断言：
//   - 切换成功（活跃视图 = B）；
//   - A 不被污染（视图/引擎历史/transcript 不含 B 内容）；
//   - 上下文不漂移（A 的会话域状态与引擎历史保持 A 自身内容）；
//   - A 随后仍能正常跑完（回复可见、状态回 idle、transcript 完整）。
func TestLongRunningSessionSurvivesViewSwitch(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()

	const aInput = "A 的长任务输入（需要跑很久）"
	const bInput = "B 的短任务输入"
	const bID = "sess-view-B"

	// 1) 长会话 A 启动并阻塞在引擎上。
	if err := service.Submit(ctx, aInput); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatal("long session A did not start")
	}
	sessions.setInfos([]SessionInfo{{ID: aID}, {ID: bID}})
	historyBefore := engine.HistoryFor(aID)
	if !service.sessions.Unit(aID).ChatState().Running {
		t.Fatal("A should be running")
	}

	// 2) 视图切换：切到空闲 B（A 继续后台运行）。
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("switch view to B: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != bID {
		t.Fatalf("active view after switch = %q, want B", got)
	}

	// 3) B 正常完成一轮。
	if err := service.Submit(ctx, bInput); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started[bID]:
	case <-time.After(3 * time.Second):
		t.Fatal("B did not start")
	}
	close(engine.release[bID])
	bDone := time.Now().Add(5 * time.Second)
	for {
		if unit := service.sessions.Unit(bID); unit == nil || !unit.ChatState().Running {
			break
		}
		if time.Now().After(bDone) {
			t.Fatalf("B did not complete:\n%s", dumpParallelState(service, engine, aID, bID))
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 4) 断言：A 未被污染、上下文不漂移。
	if !service.sessions.Unit(aID).ChatState().Running {
		t.Fatal("A stopped running while view switched to B")
	}
	historyAfter := engine.HistoryFor(aID)
	if !reflect.DeepEqual(historyAfter, historyBefore) {
		t.Fatalf("A engine history drifted after B ran:\nbefore=%+v\nafter=%+v", historyBefore, historyAfter)
	}
	if joined := joinHistoryText(historyAfter); strings.Contains(joined, bInput) {
		t.Fatalf("A engine history polluted with B input: %q", joined)
	}

	// A 视图不含 B 内容；B 视图含自身输入与回复、不含 A 输入。
	aView := readSessionView(t, service, aID)
	if strings.Contains(viewText(aView), bInput) {
		t.Fatalf("A view polluted with B input: %q", viewText(aView))
	}
	bView := readSessionView(t, service, bID)
	if !strings.Contains(viewText(bView), bInput) {
		t.Fatalf("B view missing its own input: %q", viewText(bView))
	}
	if strings.Contains(viewText(bView), aInput) {
		t.Fatalf("B view polluted with A input: %q", viewText(bView))
	}

	// transcript 按会话隔离：A 只含 A 内容，B 只含 B 内容。
	if text := joinTranscript(t, service, aID); strings.Contains(text, bInput) {
		t.Fatalf("A transcript polluted with B input: %q", text)
	}
	if text := joinTranscript(t, service, bID); !strings.Contains(text, bInput) || strings.Contains(text, aInput) {
		t.Fatalf("B transcript isolation broken: %q", text)
	}

	// 5) 释放 A：A 仍能正常跑完。
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if got := catalogStatusOf(t, service, aID); got != SessionStatusIdle {
		t.Fatalf("A status after completion = %q, want idle", got)
	}
	aView = readSessionView(t, service, aID)
	if !strings.Contains(viewText(aView), aInput) {
		t.Fatalf("A view missing its own input after completion: %q", viewText(aView))
	}
	if strings.Contains(viewText(aView), bInput) {
		t.Fatalf("A view polluted after completion: %q", viewText(aView))
	}
	if text := joinTranscript(t, service, aID); !strings.Contains(text, aInput) || !strings.Contains(text, "answer") {
		t.Fatalf("A did not complete normally (transcript missing input/reply): %q", text)
	}
}

func readSessionView(t *testing.T, service *Service, sessionID string) []Message {
	t.Helper()
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	view := service.sessionViewLocked(sessionID)
	if view == nil {
		return nil
	}
	return append([]Message(nil), view.Conversation...)
}

func viewText(messages []Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Role)
		builder.WriteString(":")
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func joinHistoryText(history []EngineMessage) string {
	var builder strings.Builder
	for _, message := range history {
		builder.WriteString(message.Role)
		builder.WriteString(":")
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func joinTranscript(t *testing.T, service *Service, sessionID string) string {
	t.Helper()
	events := service.components.tasks.TranscriptFor(sessionID)
	var builder strings.Builder
	for _, event := range events {
		builder.WriteString(event.Role)
		builder.WriteString(":")
		builder.WriteString(event.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}
