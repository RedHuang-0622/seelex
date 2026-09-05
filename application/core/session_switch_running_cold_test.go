package core

import (
	"context"
	"testing"
	"time"
)

// TestColdResumeWhileRunningIsAsync 复现“会话运行中切换到冷加载目标长期处于
// 恢复中”的链路：
//  1. 会话 A 运行中（引擎回合未 release）；
//  2. 目标 B 未驻留（冷），其存储读被门闩限速（模拟大会话的同步冷加载）；
//  3. 契约：ResumeSession(B) 不得等待门闩后的完整冷加载才返回——应立即把
//     视图切到 B 并给出权威 restoring 状态，装载在后台完成；
//  4. B 装载期间切换到热会话 C 不得被 B 的冷加载串行卡住；
//  5. B 的后台装载迟到完成不得抢占 C 的视图。
//
// 当前实现失败（RED）：冷加载在 resumeSession 内同步执行且占住视图过渡 key，
// 第一步就会阻塞超过预算，第二步在 B 释放前也无法返回。
func TestColdResumeWhileRunningIsAsync(t *testing.T) {
	engine := newMultiSessionEngine()
	now := time.Now()
	gated := &gatedHistorySessions{
		scopedSessions: &scopedSessions{
			catalog: map[string][]SessionInfo{
				"": {
					{ID: "sess-cold", Name: "cold B", UpdatedAt: now},
					{ID: "sess-b", Name: "hot C", UpdatedAt: now},
				},
			},
			histories: map[string]map[string][]EngineMessage{
				"": {
					"sess-cold": {{Role: "user", Content: "cold b content"}},
					"sess-b":    {{Role: "user", Content: "hot c content"}},
				},
			},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestService(t, engine, withTestSessions(gated))
	defer service.Shutdown()
	t.Cleanup(gated.releaseNow)

	// 会话 A 运行中：启动一次 chat 并保持引擎回合未 release。
	if err := service.Submit(context.Background(), "task in flight"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	// C 已驻留（热）；B 未注册进引擎 = 冷。
	engine.register("sess-b")
	gated.armMu.Lock()
	gated.armed = true
	gated.armMu.Unlock()

	// 第 1、2 步：运行中切换到冷会话 B，RPC 必须在预算内返回（装载在后台），
	// 快照立即反映 B + restoring。
	coldDone := make(chan error, 1)
	go func() { coldDone <- service.ResumeSession("sess-cold") }()
	select {
	case err := <-coldDone:
		if err != nil {
			t.Fatalf("ResumeSession(cold B) = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("RED REPRO: ResumeSession(cold B) blocked on synchronous cold load while A running")
	}
	// 确认后台装载确实在途（冷读已进入门闩），此时快照应已反映 B 的
	// restoring 空壳——而不是等装载全部完成才切视图。
	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("background cold load never entered gated history read")
	}
	snap := service.Snapshot()
	if snap.Session.ID != "sess-cold" {
		t.Fatalf("shell active session = %q, want sess-cold", snap.Session.ID)
	}
	if got := snap.Session.Status; got != SessionStatusRestoring {
		t.Fatalf("shell status = %q, want restoring", got)
	}

	// 第 3 步：B 冷加载尚未完成（门闩仍持），热切换 C 必须快速返回。
	hotDone := make(chan error, 1)
	go func() { hotDone <- service.ResumeSession("sess-b") }()
	select {
	case err := <-hotDone:
		if err != nil {
			t.Fatalf("ResumeSession(hot C) = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("RED REPRO: hot switch to C blocked behind cold load of B (view key serialization)")
	}
	if active := service.Snapshot().Session.ID; active != "sess-b" {
		t.Fatalf("active after C = %q, want sess-b", active)
	}

	// 第 4 步：释放 B 的冷加载门闩；B 后台完成装载但不得抢占 C 的视图。
	gated.releaseNow()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if engine.HasSession("sess-cold") || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if active := service.Snapshot().Session.ID; active != "sess-b" {
		t.Fatalf("late cold-load completion stole view: active = %q, want sess-b", active)
	}

	// 收尾：释放 A 的引擎回合并等待全部空闲。
	close(engine.release[aID])
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}
}
