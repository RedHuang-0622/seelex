package goal

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// dsa2a_test.go — DS-A2A 治理的行为验证（协议 v0.1 + 详设 §3/§5/§8）：
//   - b 独立上下文：帧 ref_seq 单调/幂等、自身回合段 append、只尾部追加；
//   - turn 跳帧：a 推进而 b 落后（Behind 可观测），回合时一次性同步追平；
//   - 缓存命中回升：相邻回合公共前缀记 cached_input_tokens（协议 §2 C3/C4）；
//   - B4 缺席：b 回合失败（429/超时）→ gate 缺席默认，a 不卡死、goal 可再驱动；
//   - 生命周期：goal 收口 → unbind(reason=done) reap；新 goal → 重新 bind；
//   - headless 冒烟：goal_tl_* RPC 可驱动完整 DS-A2A 流程并观测 b 上下文/cache/缺席。

// ---- 测试替身：可模拟 429/超时的 flaky 评估器 ----

type flakyEvaluator struct {
	mu        sync.Mutex
	replies   []TLDirective
	failFirst int // 前 N 次 Evaluate 返回 error（模拟 provider 429/超时）
	count     int
}

func newFlakyEvaluator(failFirst int, replies ...TLDirective) *flakyEvaluator {
	return &flakyEvaluator{failFirst: failFirst, replies: replies}
}

func (f *flakyEvaluator) Evaluate(_ context.Context, _ TLSessionEmbed) (TLDirective, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if f.failFirst > 0 {
		f.failFirst--
		return TLDirective{}, context.DeadlineExceeded // 429/超时语义
	}
	if len(f.replies) == 0 {
		return TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"}, nil
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	return reply, nil
}

// ---- b 上下文：帧单调 / 幂等 ----

func TestAdvisorFrameAppendMonotonicAndDup(t *testing.T) {
	peer := &AdvisorSession{State: PeerBound}
	peer.markBound("b-1", GoalFrame{ID: "g-1", Title: "目标"}, 1, 100)
	if dup, err := peer.appendFrame(Frame{Kind: FrameGoalUpdated, RefSeq: 3}); err != nil || dup {
		t.Fatalf("ref=3 应追加: dup=%v err=%v", dup, err)
	}
	// ref_seq ≤ Applied 的重发/回退 → 幂等丢弃（协议 §10 dup_frame），不报错不追加。
	dup, err := peer.appendFrame(Frame{Kind: FrameGoalUpdated, RefSeq: 2})
	if err != nil || !dup {
		t.Fatalf("ref=2 重发应幂等丢弃: dup=%v err=%v", dup, err)
	}
	// 重复帧幂等丢弃（ref == last）。
	dup, err = peer.appendFrame(Frame{Kind: FrameGoalUpdated, RefSeq: 3})
	if err != nil || !dup {
		t.Fatalf("ref=3 重复应幂等丢弃: dup=%v err=%v", dup, err)
	}
	// 跳帧合法（ref 大跳）。
	if dup, err := peer.appendFrame(Frame{Kind: FrameStepCheckpoint, RefSeq: 9}); err != nil || dup {
		t.Fatalf("跳帧 ref=9 应追加: dup=%v err=%v", dup, err)
	}
	if len(peer.Frames) != 2 || peer.Applied != 9 {
		t.Fatalf("帧账本异常: frames=%+v applied=%d", peer.Frames, peer.Applied)
	}
	if peer.RoundMemories(8) != nil && len(peer.RoundMemories(8)) != 0 {
		t.Fatal("空回合记忆应为空")
	}
}

// ---- 用户例子：a:…6,7 而 b 不动（落后）→ 命中回升 ----

func TestTurnSkipsFramesBehindAndCacheHitRise(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0) // 默认回复 checkpoint_ok
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "修复 401 竞态"}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	// round 1：checkpoint 触发 b 首次 bind（锚点）+ 首帧（全 miss）。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n1"}); err != nil {
		t.Fatalf("notify n1: %v", err)
	}
	snap := sup.Snapshot()
	if snap.RoundCount != 1 || snap.FrameCount != 1 || snap.Behind != 0 {
		t.Fatalf("round1 后 b 应有 1 回合 1 帧、水位追平: %+v", snap)
	}
	if snap.Rounds[0].CachedTokens != 0 || snap.Rounds[0].InputTokens == 0 {
		t.Fatalf("round1 应全 miss（cached=0）: %+v", snap.Rounds[0])
	}

	// a 继续推进（turn×3 = 用户例子 a:6,7）：只涨水位、不帧化、不评估 → b 落后。
	for i := 0; i < 3; i++ {
		if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
			t.Fatalf("notify turn: %v", err)
		}
	}
	snap = sup.Snapshot()
	if snap.Behind == 0 || snap.FrameCount != 1 || snap.RoundCount != 1 {
		t.Fatalf("turn 跳帧后 b 应落后且帧/回合不增: %+v", snap)
	}

	// round 2：新 checkpoint → on_eval 一次性同步（帧账本追加，Behind 追平）+ 命中回升。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n2"}); err != nil {
		t.Fatalf("notify n2: %v", err)
	}
	snap = sup.Snapshot()
	if snap.Behind != 0 || snap.RoundCount != 2 || snap.FrameCount != 2 {
		t.Fatalf("round2 后应追平且帧/回合 +1: %+v", snap)
	}
	if snap.Rounds[1].CachedTokens == 0 {
		t.Fatalf("round2 应有前缀命中（缓存命中回升）: %+v", snap.Rounds[1])
	}
	if snap.Cache.TotalCached <= 0 || snap.Cache.LastCached <= 0 || snap.Cache.HitRatio <= 0 {
		t.Fatalf("cache 汇总应显示命中回升: %+v", snap.Cache)
	}
	directives := sup.Mailbox().DrainDirectives()
	if len(directives) != 2 || directives[0].Corr == directives[1].Corr {
		t.Fatalf("两次回合应产出 corr 互异的信封指令: %+v", directives)
	}
}

// ---- B4：b 缺席（429/超时）不阻塞 a，goal 可再驱动 ----

func TestB429AbsentGateEscalatesAndRecovers(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup := NewSupervisor(ctl, newFlakyEvaluator(1, TLDirective{Kind: DirectiveVerdictDone, Content: "恢复后可收口"}),
		TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 回合失败（429/超时）→ gate 缺席默认（escalate），goal 保持 active（a 不卡死）。
	result, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "收口"})
	if err != nil {
		t.Fatalf("propose 不应因 b 缺席报错阻塞: %v", err)
	}
	if result.Outcome != OutcomeEscalate {
		t.Fatalf("b 缺席应 escalate（B4）: %+v", result)
	}
	if active, _ := ctl.ActiveGoal(); active == nil {
		t.Fatal("缺席默认后 goal 应保持 active（a 可继续）")
	}
	snap := sup.Snapshot()
	if snap.Peer != PeerReaped || snap.UnbindReason != "evicted_round_failure" {
		t.Fatalf("b 应 evicted(reason=round_failure): %+v", snap)
	}
	// b 恢复后 goal 可再驱动（新 bind + 回合，不残留孤儿态）。
	directive, err := sup.RunEval(testCtx, "recovery")
	if err != nil {
		t.Fatalf("恢复回合失败: %v", err)
	}
	if directive.Kind != DirectiveVerdictDone || directive.Corr == "" {
		t.Fatalf("恢复回合应产出裁决: %+v", directive)
	}
	snap = sup.Snapshot()
	if snap.Peer == PeerReaped || snap.RoundCount != 1 || snap.FrameCount == 0 {
		t.Fatalf("b 应重建并执行 1 回合: %+v", snap)
	}
}

// ---- 生命周期：收口 → reap；新 goal → 重新 bind ----

func TestGoalFinishReapsAndRebinds(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveVerdictDone, Content: "done"})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "g1"}); err != nil {
		t.Fatalf("begin g1: %v", err)
	}
	if _, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "收口"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if snap := sup.Snapshot(); snap.Peer != PeerReaped || snap.UnbindReason != "done" {
		t.Fatalf("g1 收口后 b 应 reaped(done): %+v", snap)
	}
	// 新 goal → 重新 bind（新 peer，独立上下文从头累积）。
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "g2"}); err != nil {
		t.Fatalf("begin g2: %v", err)
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n-g2"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	snap := sup.Snapshot()
	if snap.Peer == PeerReaped || snap.ActiveGoalTitle != "g2" || snap.FrameCount == 0 || snap.RoundCount != 1 {
		t.Fatalf("g2 应重新 bind 并回合: %+v", snap)
	}
}

// ---- headless 冒烟：DS-A2A 治理经 goal_tl_* RPC 全流程可观测 ----

func TestHeadlessDSA2AAdvisorSmoke(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	stub := newStubEvaluator() // 默认 checkpoint_ok
	sup := NewSupervisor(ctl, stub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	server := httptest.NewServer(NewServer(ctl).WithTechLeader(sup).Handler())
	t.Cleanup(server.Close)
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := client.Call(ctx, "goal_begin", BeginRequest{Title: "发布 v1"}, nil); err != nil {
		t.Fatalf("goal_begin: %v", err)
	}
	// round1：checkpoint → b bind + 帧。
	if err := client.Call(ctx, "goal_tl_notify", TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n1"}, nil); err != nil {
		t.Fatalf("notify: %v", err)
	}
	var snap TLState
	if err := client.Call(ctx, "goal_tl_snapshot", nil, &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Peer == PeerReaped || snap.RoundCount != 1 || snap.Cache.LastCached != 0 {
		t.Fatalf("round1 后 peer/round/cache 异常: %+v", snap)
	}
	// a 推进 turn ×3：b 落后可观测（headless 也可看到 behind）。
	for i := 0; i < 3; i++ {
		if err := client.Call(ctx, "goal_tl_notify", TLEvalSignal{Kind: SignalTurnCompleted}, nil); err != nil {
			t.Fatalf("notify turn: %v", err)
		}
	}
	if err := client.Call(ctx, "goal_tl_snapshot", nil, &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Behind == 0 {
		t.Fatalf("turn 跳帧后应显示 b 落后: %+v", snap)
	}
	// round2：compacted → 命中回升。
	if err := client.Call(ctx, "goal_tl_notify", TLEvalSignal{Kind: SignalContextCompacted}, nil); err != nil {
		t.Fatalf("notify compacted: %v", err)
	}
	snap = TLState{} // wire 0 值字段也会下发；重置避免复用残留
	if err := client.Call(ctx, "goal_tl_snapshot", nil, &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.RoundCount != 2 || snap.Behind != 0 || snap.Cache.LastCached == 0 || snap.Cache.HitRatio <= 0 {
		t.Fatalf("round2 后命中回升/追平异常: %+v", snap)
	}
	// b→a 指令信封（corr）可排空。
	var directives []TLDirective
	if err := client.Call(ctx, "goal_tl_directives", nil, &directives); err != nil {
		t.Fatalf("directives: %v", err)
	}
	if len(directives) != 2 || directives[0].Corr == "" || directives[1].Corr == "" || directives[0].Corr == directives[1].Corr {
		t.Fatalf("corr 信封异常: %+v", directives)
	}
}

// ---- headless 冒烟：B4 缺席（429）经 RPC 不阻塞 goal 驱动 ----

func TestHeadlessDSA2AAbsentGate(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup := NewSupervisor(ctl, newFlakyEvaluator(1, TLDirective{Kind: DirectiveVerdictDone, Content: "ok"}),
		TechLeaderConfig{Enabled: true, EvalWindow: 0})
	server := httptest.NewServer(NewServer(ctl).WithTechLeader(sup).Handler())
	t.Cleanup(server.Close)
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := client.Call(ctx, "goal_begin", BeginRequest{Title: "目标"}, nil); err != nil {
		t.Fatalf("goal_begin: %v", err)
	}
	var proposal FinishProposalResult
	// b 回合 429 → RPC 仍返回结构化缺席结果（escalate），不阻塞调用方。
	if err := client.Call(ctx, "goal_propose_finish", FinishRequest{Result: "收口"}, &proposal); err != nil {
		t.Fatalf("propose_finish 不应报错: %v", err)
	}
	if proposal.Outcome != OutcomeEscalate {
		t.Fatalf("b 缺席应 escalate: %+v", proposal)
	}
	var status StatusView
	if err := client.Call(ctx, "goal_status", nil, &status); err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	if status.Active == nil {
		t.Fatal("缺席后 goal 应保持 active（可转人工/续跑）")
	}
}
