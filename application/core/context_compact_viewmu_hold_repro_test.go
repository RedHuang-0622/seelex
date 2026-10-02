package core

// 复现（一）：**压缩在持有 Core.ViewMu 的临界区里做推帧**——锁的持有时间 = 推帧
// 的耗时。推帧在生产路径上不是纯计算：
//
//   - seelebridge/runtime_compaction_index.go 先跑 MainCompactionDAG：
//     DAG 的 chapter2 节点在 limits.context_compaction_summary.enabled 时做**前缀重放
//     厚摘要**（整段历史重放的模型调用；溢出区超过片预算时逐片重放、多次调用）；
//   - 再走归档器 CompressedTurnArchiver.StoreTurn（原文写盘）与 store.PushCompact
//     （压缩栈写盘）。
//
// 这一段原先落在 coordinator.go 单个 `c.ViewMu.Lock()`…`c.ViewMu.Unlock()` 临界区
// 里（推帧调用点在临界区内部），于是推帧没回来之前：
//
//	/compact 的 RPC 不返回 → GUI 不清空输入框（清空动作在 await 之后）；
//	Snapshot（快照/列表/右栏）取不到读锁 → 会话切不动；
//	Submit 的第一步就取 ViewMu.RLock → 消息连队列都进不去；
//	进度条停在 replace 关（3/7）——index 关要等推帧返回才收口。
//
// 修法（2026-09-29）：临界区拆成三段——锁内提交状态（A）→ **锁外**推帧与渲染
// （B）→ 锁内落存储与写记录（C）。于是推帧进行中只有"这一次压缩自己"在等，
// 交互面（快照/提交/切会话/写锁本身）不再被扣住。
//
// 本用例用"推帧可阻塞"的索引面桩把这段耗时变成可控的判定点：判据 = 推帧进行中
// 那四个交互入口是否照常（任一被冻 = 缺陷还在）；/compact 是否仍等自己的推帧
// 则是**契约断言**（回执要嵌进记录与帧正文，不许提前返回）。

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// blockingIndexRuntime 是"索引面在、但推帧这一步要跑很久"的 Runtime 桩：进到
// PushCompactionFrame 就通知测试，并阻塞到 release 才返回。
type blockingIndexRuntime struct {
	runtimeWithContextLimits
	entered   chan struct{}
	release   chan struct{}
	enteredOn sync.Once
	ctxSeen   chan context.Context
}

func (runtime *blockingIndexRuntime) PushCompactionFrame(
	ctx context.Context,
	_ string,
	_ context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	runtime.enteredOn.Do(func() { close(runtime.entered) })
	select {
	case runtime.ctxSeen <- ctx:
	default:
	}
	<-runtime.release
	return context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-blocked-1",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n阻塞复核",
		SummarySource: "replay",
	}, nil
}

// returnsWithin 报告 fn 是否在 d 之内返回（返回 false = 阻塞住了）。
func returnsWithin(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestExplicitCompactReproViewMuHoldAcrossFramePush：推帧进行中，交互面是否仍然可用。
//
// 当前代码（e095068 起）会把 ViewMu 连同推帧一起扣住：本用例以 **Skip + 证据** 报告
// 复现（含"推帧拿到的 ctx 不可取消"，见下）。修好之后同一条用例自动转为**存活断言**
// ——推帧进行中快照/提交/切会话都必须照常，推帧结束整轮门禁走满 7 关。
func TestExplicitCompactReproViewMuHoldAcrossFramePush(t *testing.T) {
	runtime := &blockingIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{
			fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
		ctxSeen: make(chan context.Context, 1),
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-viewmu"}
	service.components.tasks.BeginTask("task-viewmu", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	// 4 个已定稿大轮（约 16 万 tokens）：越软阈值，显式压缩会折出非空溢出区，
	// 压缩才会走到推帧那一关。
	appendIndexRounds(t, service, "task-viewmu")
	sessionID := service.Snapshot().Session.ID

	// 无论断言怎么走，退出前一律释放推帧：本例的冻结必须是可控的（真正的永久
	// 自锁在另一条用例里），否则 t.Cleanup 的 Shutdown 会跟着挂死。
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(runtime.release) }) }
	defer release()

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	// 用户按回车提交 `/compact`：与 GUI 同一条链路（Bridge.Submit → Service.Submit
	// → input_router → submitCommand → 命令 → CompactContextNow），同步跑在 RPC 的
	// 那个 goroutine 上。
	commandDone := make(chan error, 1)
	commandReturned := make(chan struct{})
	go func() {
		commandDone <- service.Submit(context.Background(), "/compact")
		close(commandReturned)
	}()

	select {
	case <-runtime.entered:
	case err := <-commandDone:
		t.Fatalf("压缩在进入推帧之前就返回了，夹具没走到要复现的那一步：%v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("5s 内没有进入推帧（夹具失效）")
	}

	// 判定面：推帧进行中，交互面还能不能动。前四项是「锁被推帧扣住」的直接症状
	// ——任一项为真，缺陷就还在（第一项直接探写锁本身，是最直白的判据：推帧是否
	// 落在 Core.ViewMu 临界区内）。
	//
	// commandBlocked 单列、且**不算冻结症状**：/compact 等的是自己这一次推帧
	// （同一 goroutine 的同步调用；回执要嵌进帧正文与压缩记录，不能不等），推帧
	// 被桩按住时它必然不返回。这条路径的契约是「推帧没回来，命令不许回来」，因此
	// 它在这里是**契约断言**（见下方 if !commandBlocked），不是缺陷证据。
	muBlocked := !returnsWithin(time.Second, func() {
		service.ViewMu.Lock()
		service.ViewMu.Unlock()
	})
	snapshotBlocked := !returnsWithin(time.Second, func() { _ = service.Snapshot() })
	submitBlocked := !returnsWithin(time.Second, func() {
		_ = service.Submit(context.Background(), "hello")
	})
	switchBlocked := !returnsWithin(time.Second, func() { _ = service.resumeSession("session-other") })
	commandBlocked := !returnsWithin(time.Second, func() { <-commandReturned })

	if muBlocked || snapshotBlocked || submitBlocked || switchBlocked {
		frames := drainCompactionProgress(t, subscription)
		lastGate, lastIndex := "", 0
		if len(frames) > 0 {
			lastGate = frames[len(frames)-1].event.Gate
			lastIndex = frames[len(frames)-1].event.Index
		}
		evidence := []string{
			"进度条停在 " + lastGate + "(" + strconv.Itoa(lastIndex) + "/7)：index 关要等推帧返回",
			"推帧期间 ViewMu 写锁拿不到=" + strconv.FormatBool(muBlocked) + "（推帧确实在临界区内）",
			"Snapshot 被冻=" + strconv.FormatBool(snapshotBlocked) + "（会话切不动/列表不刷新）",
			"新消息被冻=" + strconv.FormatBool(submitBlocked) + "（进不了消息队列）",
			"切会话被冻=" + strconv.FormatBool(switchBlocked),
		}
		if ctx, ok := <-runtime.ctxSeen; ok && ctx.Done() == nil {
			evidence = append(evidence, "推帧 ctx 不可取消（context.Background，见 compaction_index.go:68）："+
				"这一轮只能等推帧自己回来（交互面已不受影响时不算缺陷，见本用例的判据面）")
		}
		t.Fatalf("压缩在持有 Core.ViewMu 期间推帧，锁被推帧扣住（交互面被冻）：%s", strings.Join(evidence, "；"))
	}

	// 推帧没回来之前 /compact 不得返回：压缩记录与帧正文都要嵌推帧回执
	// （segment_id / 摘要来源 / 降级原因），提前返回就会留下一份没有细筛入口的
	// 记录与帧正文（而原文其实已经归档，只是没人拿得到 segment_id）。
	if !commandBlocked {
		t.Fatal("推帧尚未返回，/compact 已返回（记录与帧正文会缺 segment_id）")
	}

	// 推帧进行中，交互面必须照常（这条路径修好后的期望形状）。
	release()
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatalf("/compact 释放后应正常返回：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("/compact 释放推帧后仍未返回")
	}
	frames := append([]compactionProgressFrame(nil), drainCompactionProgress(t, subscription)...)
	sequence := gateSequence(frames)
	if len(sequence) != context_runtime.CompactionGateTotal() {
		t.Fatalf("一轮压缩应走满 %d 关，实际 %d 关：%v",
			context_runtime.CompactionGateTotal(), len(sequence), sequence)
	}
	for index, gate := range context_runtime.CompactionGates {
		if sequence[index] != gate {
			t.Fatalf("门禁第 %d 关 = %q，want %q（顺序 %v）", index+1, sequence[index], gate, sequence)
		}
	}
}
