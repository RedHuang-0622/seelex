package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// attachFailingSessions 是 scopedSessions 的一个变体：实现
// session_runtime.SessionContextPort，并对指定会话的 context 挂接返回错误
// （模拟冷恢复的“迟到失败”——视图激活之后才出错）。
type attachFailingSessions struct {
	*scopedSessions
	failFor map[string]bool
}

func (s *attachFailingSessions) AttachSessionContext(_ string, sessionID string) error {
	if s.failFor[sessionID] {
		return errors.New("attach session context failed (repro: late cold-resume failure)")
	}
	return nil
}

func (s *attachFailingSessions) DetachSessionContext() {}

// TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt 复现“切换失败
// 之后继续在当前会话输入修正”链路：
//
//  1. 会话 A 驻留且空闲（视图 = A）；目标 B 冷（未驻留），其历史/目录存在，
//     但 B 的 context 挂接（AttachSessionContext）失败——该步骤发生在视图激活
//     之后（迟到失败）；
//  2. ResumeSession(B) 返回错误，但**视图已被切到 B**；前端错误路径只提示不
//     刷新，用户仍以为在 A，随后在 A 视图输入修正 → 输入被路由进 B ——
//     表现为“切换失败后内容发不出去 / 发错会话”；
//  3. 修复后不变量：ResumeSession 返回错误 ⇒ 视图仍停留在切换前会话 A（与后台
//     冷加载失败路径 handleColdRestoreFailure 同一语义）；
//  4. 修复后在 A 的输入必须正常路由到 A 并完成一轮对话。
//
// 当前实现失败（RED）：同步冷加载迟到失败不回滚视图。
func TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt(t *testing.T) {
	engine := newMultiSessionEngine()
	now := time.Now()
	store := &attachFailingSessions{
		scopedSessions: &scopedSessions{
			catalog: map[string][]SessionInfo{
				"": {{ID: "sess-b", Name: "cold B", UpdatedAt: now}},
			},
			histories: map[string]map[string][]EngineMessage{
				"": {"sess-b": {{Role: "user", Content: "cold b content"}}},
			},
		},
		failFor: map[string]bool{"sess-b": true},
	}
	service := newTestService(t, engine, withTestSessions(store))
	defer service.Shutdown()

	ctx := context.Background()
	// 会话 A：提交首条 → 引擎会话物化并完成（A 驻留、空闲、视图 = A）。
	if err := service.Submit(ctx, "hello A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])
	close(engine.release[aID])
	waitForChatCompletion(t, service)
	if !engine.HasSession(aID) {
		t.Fatalf("session A not resident after first submit")
	}
	if got := service.Snapshot().Session.ID; got != aID {
		t.Fatalf("active before failed resume = %q, want %q", got, aID)
	}

	// 目标 B 冷（未注册进引擎）；恢复失败发生在视图激活之后。
	if err := service.ResumeSession("sess-b"); err == nil {
		t.Fatalf("ResumeSession(cold B) expected an error from the failing context attach")
	}

	// 不变量：ResumeSession 返回错误 ⇒ 视图仍停留在切换前会话 A。
	if got := service.Snapshot().Session.ID; got != aID {
		t.Fatalf("RED REPRO: after failed ResumeSession(B) view = %q, want still %q (input would route to B while user sees A)", got, aID)
	}

	// 用户（仍看到 A）输入修正：必须路由到 A 并正常完成一轮对话。
	if err := service.Submit(ctx, "correction for A"); err != nil {
		t.Fatalf("submit correction after failed resume: %v", err)
	}
	waitForChatCompletion(t, service)
	snap := service.Snapshot()
	if snap.Session.ID != aID {
		t.Fatalf("active after correction = %q, want %q", snap.Session.ID, aID)
	}
	found := false
	for _, message := range snap.Conversation {
		if strings.Contains(message.Content, "correction for A") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("correction missing from session %s conversation: %+v", aID, snap.Conversation)
	}
}
