package goal

import (
	"errors"
	"testing"
)

// 这些用例验证 DS-A2A 编排语义（替换旧"同会话共享"语义的监督器测试）：
//   - turn_completed 只推进 a 事件水位、跳帧不评估（b 落后可观测）；
//   - step_checkpoint 受 eval_window 抑制，到窗触发 b 回合（帧 append 到 b 上下文）；
//   - 关键信号（compacted/approval/terminal）立即回合；
//   - b 回合输入 = 锚点 + 追加帧（ref_seq 单调）+ 自身回合记忆，不含 a 实时尾窗；
//   - b 产物走 corr 信封（DirectiveBus），不再写回 goal 共享指令环；
//   - 无 active goal / TL 未启用 / 非法输出的边界保持不变。

// TestTurnCompletedNeverEvals 验证 turn 跳帧：只计数不评估（用户例子中 a:6,7 而 b 不动）。
func TestTurnCompletedNeverEvals(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	for index := 0; index < 6; index++ {
		if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	if stub.evalCount() != 0 {
		t.Fatalf("turn_completed 不应触发评估, 得 %d", stub.evalCount())
	}
	if snapshot := sup.Snapshot(); snapshot.TurnsSinceEval != 6 {
		t.Fatalf("窗口计数应为 6, 得 %d", snapshot.TurnsSinceEval)
	}
}

// TestEvalWindowSkipsAndFires 验证 eval_window（D5）：step 窗口内抑制、期满触发；
// 回合产物 = corr 信封指令，且不回写 goal 共享状态。
func TestEvalWindowSkipsAndFires(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 3,
		TLDirective{Kind: DirectiveCorrect, Content: "先补验证再继续"})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n1"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if stub.evalCount() != 0 {
		t.Fatalf("窗口内 step 不应评估, 得 %d", stub.evalCount())
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n2"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if stub.evalCount() != 1 {
		t.Fatalf("窗口期满 step 应触发 b 回合, 得 %d", stub.evalCount())
	}

	// b 产物 = corr 信封（DirectiveBus 一次性排空）。
	directives := sup.Mailbox().DrainDirectives()
	if len(directives) != 1 || directives[0].Kind != DirectiveCorrect {
		t.Fatalf("指令未发布: %+v", directives)
	}
	if directives[0].Corr == "" {
		t.Fatal("b→a 指令应带 corr 信封（幂等/审计）")
	}
	snapshot := sup.Snapshot()
	if snapshot.RoundCount != 1 || snapshot.FrameCount == 0 || snapshot.Behind != 0 {
		t.Fatalf("b 上下文应有 1 回合 + 帧账本、水位追平: %+v", snapshot)
	}
	if snapshot.Rounds[0].Corr != directives[0].Corr {
		t.Fatalf("回合 corr 与指令信封不一致: %+v vs %+v", snapshot.Rounds[0], directives[0])
	}
	// DS-A2A：TL 产物不再写回 goal 共享指令环（同会话写污染已移除）。
	active, _ := ctl.ActiveGoal()
	if len(active.Directives) != 0 {
		t.Fatalf("b 回合不应写 goal 指令环（共享状态污染）: %+v", active.Directives)
	}
}

// TestCriticalSignalImmediateEval 验证关键信号绕过窗口立即评估。
func TestCriticalSignalImmediateEval(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 100,
		TLDirective{Kind: DirectiveCorrect, Content: "压缩后重锚目标"})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	for index := 0; index < 10; index++ {
		if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalContextCompacted}); err != nil {
		t.Fatalf("notify compacted: %v", err)
	}
	if stub.evalCount() != 1 {
		t.Fatalf("context_compacted 应立即评估, 得 %d", stub.evalCount())
	}
}

// TestEmbedCarriesAnchorAndFrames 验证 b 回合输入来自 b 自身上下文：
// 锚点 goal.start 快照（bind 时一次），其后 a 的变化以 goal.update 差异帧 + 触发帧
// （context.compacted）append 进 b 帧账本（ref_seq 单调），不含 a 实时尾窗。
func TestEmbedCarriesAnchorAndFrames(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"},
		TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"})
	if _, err := ctl.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Statement: "收敛版本并带测试",
		Acceptance: []string{"go test 全绿", "README 更新"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 回合 1：bind（锚点 = begin 时快照）。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n-bind"}); err != nil {
		t.Fatalf("notify checkpoint: %v", err)
	}
	// bind 之后 a 再 update → 差异由下个回合前的 goal.update 帧补上（on_eval 同步）。
	if _, err := ctl.Update(testCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "框架装配完成",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	for index := 0; index < 5; index++ {
		if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	// 回合 2：关键信号 context_compacted → 立即回合（含 goal.update 补帧）。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalContextCompacted}); err != nil {
		t.Fatalf("notify compacted: %v", err)
	}

	embed := stub.last()
	if embed == nil {
		t.Fatal("b 未执行回合")
	}
	if embed.PeerID == "" || embed.Goal.ID == "" {
		t.Fatalf("回合输入缺 peer/锚点: %+v", embed)
	}
	if embed.Goal.Title != "发布 v1" || embed.Goal.Statement != "收敛版本并带测试" {
		t.Fatalf("锚点丢失 goal 帧: %+v", embed.Goal)
	}
	if len(embed.Goal.Acceptance) != 2 || embed.Goal.Acceptance[0] != "go test 全绿" {
		t.Fatalf("锚点丢失 acceptance: %+v", embed.Goal.Acceptance)
	}
	// 追加帧：goal.update 补帧（bind 后变化）+ context.compacted 触发帧。
	hasUpdate, hasCompact := false, false
	for _, frame := range embed.Frames {
		switch frame.Kind {
		case FrameGoalUpdated:
			hasUpdate = true
		case FrameContextCompacted:
			hasCompact = true
		}
	}
	if !hasUpdate || !hasCompact {
		t.Fatalf("追加帧应含 goal.update 与 context.compacted: %+v", embed.Frames)
	}
	for index := 1; index < len(embed.Frames); index++ {
		if embed.Frames[index].RefSeq <= embed.Frames[index-1].RefSeq {
			t.Fatalf("帧 ref_seq 须单调: %+v", embed.Frames)
		}
	}
	if err := embed.Validate(); err != nil {
		t.Fatalf("回合输入不合法: %v", err)
	}
}

// TestNoActiveGoalNoEval 验证无 goal 时 Notify 静默忽略、RunEval 明确报错。
func TestNoActiveGoalNoEval(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0)
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalContextCompacted}); err != nil {
		t.Fatalf("无 goal notify 应静默忽略, 得 %v", err)
	}
	if stub.evalCount() != 0 {
		t.Fatalf("无 goal 不应评估, 得 %d", stub.evalCount())
	}
	if _, err := sup.RunEval(testCtx, "manual"); !errors.Is(err, ErrNoActiveGoal) {
		t.Fatalf("无 goal RunEval 应报 ErrNoActiveGoal, 得 %v", err)
	}
}

// TestTLDisabledFallback 验证 TL 未启用（无评估器）时不评估、gate 走缺席直连。
func TestTLDisabledFallback(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup := NewSupervisor(ctl, nil, TechLeaderConfig{Enabled: true})
	if sup.Enabled() {
		t.Fatal("无评估器应视为未启用")
	}
	if _, err := sup.RunEval(testCtx, "x"); !errors.Is(err, ErrTLDisabled) {
		t.Fatalf("未启用 RunEval 应报 ErrTLDisabled, 得 %v", err)
	}
}

// TestBadDirectiveRejected 验证 b 输出非法/超长/漂移指令被拒。
func TestBadDirectiveRejected(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	longReply := newStubEvaluator(TLDirective{Kind: DirectiveCorrect, Content: string(make([]rune, MaxDirectiveRunes+1))})
	sup := NewSupervisor(ctl, longReply, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup.RunEval(testCtx, "x"); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("超长指令应报 ErrBadDirective, 得 %v", err)
	}
	bogus := newStubEvaluator(TLDirective{Kind: "wat", Content: "x"})
	sup2 := NewSupervisor(ctl, bogus, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup2.RunEval(testCtx, "x"); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("非法 kind 应报 ErrBadDirective, 得 %v", err)
	}
	drift := newStubEvaluator(TLDirective{Kind: DirectiveCorrect, Content: "x", GoalID: "g-other"})
	sup3 := NewSupervisor(ctl, drift, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup3.RunEval(testCtx, "x"); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("goal 漂移应报 ErrBadDirective, 得 %v", err)
	}
}

// TestDirectiveEventAndRing 验证 Controller.AppendDirective 仍提供（审计/兼容），但 DS-A2A
// 回合不再调用它（见 TestEvalWindowSkipsAndFires 的"不回写"断言）。
func TestDirectiveEventAndRing(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sub := ctl.Subscribe(8)
	defer sub.Close()
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	for index := 0; index < MaxDirectives+3; index++ {
		if _, err := ctl.AppendDirective(testCtx, "directive-n"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	active, _ := ctl.ActiveGoal()
	if len(active.Directives) != MaxDirectives {
		t.Fatalf("指令环应封顶 %d, 得 %d", MaxDirectives, len(active.Directives))
	}
	found := false
	for {
		select {
		case event := <-sub.Events:
			if event.Kind == EventDirective {
				found = true
			}
		default:
			if !found {
				t.Fatal("事件流未见 goal.directive")
			}
			return
		}
	}
}
