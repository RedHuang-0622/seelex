package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 同会话压缩门（context_compact_gate.go）的串行语义：一轮压缩在跑时，第二次显式
// 压缩**等它收口**再执行，而不是并发压缩——两边各自读同一段历史、各自替换，后写的
// 那份会把前一轮整个丢掉。
//
// 这条现在对**所有**调用方成立，包括落在正在跑的回合里的调用（compact_context 工具、
// 回合内的 /compact）：Seele 把回合准入换成闸门 + 工作状态短临界区之后，回合进行中
// 不再持有跨整轮的会话锁，`History`/`ReplaceHistory` 也不等回合（忙会话的替换排队到
// 下一个检查点）。于是"回合内的调用方等门"与"锁外那轮读历史"之间不存在互等。
//
// 历史沿革：旧模型下回合内持着 Session.mu，等门会与锁外那轮互等成死锁，因此当时只在
// 回合内做非阻塞领轮（领不到就如实报错）。那条补丁连同它的判据（引擎侧的
// InLoopTurn / 环内把手）已随 Seele 升级一起删除——本文件钉的是删掉之后的具体行为。

// seedLongRounds 与 TestCompactContextHandlerCompactsTranscript 同一份量：4 轮、每轮
// 16 万字符，稳过软阈值，保证压缩真的发生。
func seedLongRounds(t *testing.T, service *Service, requestID string) {
	t.Helper()
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	for index := 0; index < 4; index++ {
		body := "round-" + string(rune('a'+index)) + ":" + strings.Repeat("A", 160_000)
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: requestID, Role: "user", Content: "q-" + string(rune('a'+index)),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: requestID, Role: "assistant", Content: body,
		})
	}
}

// TestCompactContextWaitsForHeldCompactionGate 钉住等待方向：门已被占用时，第二次
// 显式压缩**等待**（不是并发压缩、也不是立刻报错），收口后照常完成并自己收口。
func TestCompactContextWaitsForHeldCompactionGate(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-gate-wait")
	seedLongRounds(t, service, "task-gate-wait")
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = sessionID
	service.ViewMu.Unlock()
	ctx := withSessionID(context.Background(), sessionID)

	// 预置：另一个调用方已经领到这一会话的压缩轮（它正在压缩中）。
	// 与被删掉的 tryAcquireCompactionRound 做的事完全一致——生产代码里没有
	// 「非阻塞领轮」这条路径了（见 gate 文件的加锁纪律注释）。
	service.ViewMu.Lock()
	if service.compacting == nil {
		service.compacting = map[string]struct{}{}
	}
	service.compacting[sessionID] = struct{}{}
	service.ViewMu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := service.CompactContextNow(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("门被占用时压缩不该立刻返回（err=%v）", err)
	case <-time.After(200 * time.Millisecond):
	}

	service.releaseCompactionRound(sessionID)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("门收口后压缩失败：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("门收口后压缩没有继续")
	}
	if service.isCompactingLocked(sessionID) {
		t.Fatal("压缩轮没有收口：后续压缩会一直等下去")
	}

	// 收口之后同一会话还能再压一次（门没有留在坏状态）。
	second := make(chan error, 1)
	go func() {
		_, err := service.CompactContextNow(ctx)
		second <- err
	}()
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("第二次压缩失败：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("第二次压缩没有完成")
	}
}
