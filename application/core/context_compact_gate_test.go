package core

import (
	"context"
	"testing"
	"time"
)

// 同会话压缩串行门的三条判据（实现见 context_compact_gate.go）。
//
// 它们钉的是同一个事故：没有在执行纪元的显式压缩（冷加载/刚清空的会话）刻意不
// 写 ChatState.Running，因此 Submit 的 busy 判据看不见这一轮。缺门时的现场是
// "折叠读完引擎历史 → 新回合开出并装配 → 两边各自替换历史 → 后写的把折叠丢掉"，
// 用户看到的是一条压完就消失的记录加上一条按旧上下文回答的回复。

func engineChatInputs(engine *fakeEngine) []string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]string(nil), engine.chatInputs...)
}

// waitForChatInputs 轮询到引擎收到的请求数达到 want，返回这些请求。
func waitForChatInputs(t *testing.T, engine *fakeEngine, want int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if inputs := engineChatInputs(engine); len(inputs) >= want {
			return inputs
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待 3s 后引擎只收到 %d 次请求，期望 %d 次", len(engineChatInputs(engine)), want)
	return nil
}

// TestSubmitParksWhileCompactionRoundOpen 是这条不变量的正面：压缩轮进行中，
// 同会话的提交**照常被受理但不成回合**（没有新的引擎请求），轮次收口后在同
// 一会话上自动补投。补投不能靠队列：显式压缩没有回合，队列的正常提升点
// （回合结束）永远不会到来。
func TestSubmitParksWhileCompactionRoundOpen(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	ctx := context.Background()
	if err := service.Submit(ctx, "first"); err != nil {
		t.Fatalf("首次提交：%v", err)
	}
	waitForChatInputs(t, engine, 1)
	// 门只管"没有回合在跑"的窗口：等第一轮收尾，排除排队分支的干扰。
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return !snapshot.Chat.Running })

	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("夹具没有视图会话 ID")
	}
	if err := service.acquireCompactionRound(ctx, sessionID); err != nil {
		t.Fatalf("领取压缩轮：%v", err)
	}
	if err := service.Submit(ctx, "parked-while-compacting"); err != nil {
		service.releaseCompactionRound(sessionID)
		t.Fatalf("压缩进行中提交应照常受理：%v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if inputs := engineChatInputs(engine); len(inputs) != 1 {
		service.releaseCompactionRound(sessionID)
		t.Fatalf("压缩轮进行中开出了新回合：engine 请求 %d 次（期望仍是 1）", len(inputs))
	}
	service.releaseCompactionRound(sessionID)
	waitForChatInputs(t, engine, 2)
}

// TestCompactionGateIsScopedToItsSession 钉住"同会话串行"里的另一半：门按会话
// 键控，别的会话照常开回合（进程级一把锁会在第一个断言处失败）。
func TestCompactionGateIsScopedToItsSession(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	ctx := context.Background()
	if err := service.acquireCompactionRound(ctx, "sess-being-compacted"); err != nil {
		t.Fatalf("领取压缩轮：%v", err)
	}
	t.Cleanup(func() { service.releaseCompactionRound("sess-being-compacted") })

	if err := service.Submit(ctx, "other-session-input"); err != nil {
		t.Fatalf("提交：%v", err)
	}
	waitForChatInputs(t, engine, 1)
}

// TestCompactionRoundIsAcquiredExclusively 是"串行"那一半：上一轮没收口时，下一
// 次显式压缩不会并行开跑（两条折叠并行 = 两次历史替换互相覆盖，且门禁进度条会
// 出现两套并存的格子），收口后立即兑现。
func TestCompactionRoundIsAcquiredExclusively(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	ctx := context.Background()
	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("夹具没有视图会话 ID")
	}
	if err := service.acquireCompactionRound(ctx, sessionID); err != nil {
		t.Fatalf("领取压缩轮：%v", err)
	}

	type compactCall struct {
		result ContextCompactionResult
		err    error
	}
	returned := make(chan compactCall, 1)
	go func() {
		result, err := service.CompactContextNow(ctx)
		returned <- compactCall{result: result, err: err}
	}()
	select {
	case call := <-returned:
		service.releaseCompactionRound(sessionID)
		t.Fatalf("上一轮还没收口，第二次压缩就返回了：%+v err=%v", call.result, call.err)
	case <-time.After(80 * time.Millisecond):
	}

	service.releaseCompactionRound(sessionID)
	select {
	case call := <-returned:
		if call.err != nil {
			service.releaseCompactionRound(sessionID)
			t.Fatalf("收口后第二次压缩应兑现：%v", call.err)
		}
	case <-time.After(3 * time.Second):
		service.releaseCompactionRound(sessionID)
		t.Fatal("收口后第二次压缩没有兑现（等待方没被唤醒或唤醒后没复判）")
	}
	// 收口必须成对：门不能泄漏，否则这个会话之后每次提交都挂在等待里。
	service.ViewMu.RLock()
	leaked := service.isCompactingLocked(sessionID)
	service.ViewMu.RUnlock()
	if leaked {
		t.Fatalf("压缩轮收口后 compacting 仍留着会话 %s", sessionID)
	}
}
