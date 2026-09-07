package goal

import (
	"errors"
	"testing"
)

// TestTurnCompletedNeverEvals 验证 turn_completed 只计数不触发评估（design §5.2）。
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

// TestEvalWindowSkipsAndFires 验证 eval_window（D5）：
// step_checkpoint 在窗口内被抑制，窗口期满后触发。
func TestEvalWindowSkipsAndFires(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 3,
		TLDirective{Kind: DirectiveCorrect, Content: "先补验证再继续"})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 1 轮 turn 后 step → 窗口 1<3 → 抑制。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n1"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if stub.evalCount() != 0 {
		t.Fatalf("窗口内 step 不应评估, 得 %d", stub.evalCount())
	}
	// 再 2 轮 turn 使计数到 3 → step 触发。
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
		t.Fatalf("窗口期满 step 应评估, 得 %d", stub.evalCount())
	}
	directives := sup.Mailbox().DrainDirectives()
	if len(directives) != 1 || directives[0].Kind != DirectiveCorrect {
		t.Fatalf("指令未发布: %+v", directives)
	}
	// 指令摘要已写入 goal 指令环（Goal 帧/TLMemory 素材）。
	active, _ := ctl.ActiveGoal()
	if len(active.Directives) != 1 || active.Directives[0] != directives[0].Summary() {
		t.Fatalf("goal 指令环未写入: %+v", active.Directives)
	}
}

// TestCriticalSignalImmediateEval 验证关键信号绕过窗口立即评估。
func TestCriticalSignalImmediateEval(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 100, // 大窗口也不该抑制关键信号
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

// TestEmbedAlwaysCarriesGoalFrame 验证防遗忘约束（design §5.3/§9）：
// 多次 turn 与一次 context_compacted 后，TL 回合嵌入仍含完整 goal 帧
// （statement/acceptance/status/最近 progress）。
func TestEmbedAlwaysCarriesGoalFrame(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"})
	if _, err := ctl.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Statement: "收敛版本并带测试",
		Acceptance: []string{"go test 全绿", "README 更新"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := ctl.Update(testCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "框架装配完成",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	sup.SetSessionTail([]TurnBrief{
		{Role: "mainagent", Summary: "跑测试"},
		{Role: "tool", Summary: "go test 通过", Tool: "bash"},
	})
	for index := 0; index < 5; index++ {
		if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalContextCompacted}); err != nil {
		t.Fatalf("notify compacted: %v", err)
	}

	embed := stub.last()
	if embed == nil {
		t.Fatal("TL 未执行回合")
	}
	if embed.Goal.ID == "" || embed.Goal.Title != "发布 v1" || embed.Goal.Statement != "收敛版本并带测试" {
		t.Fatalf("嵌入丢失 goal 帧: %+v", embed.Goal)
	}
	if len(embed.Goal.Acceptance) != 2 || embed.Goal.Acceptance[0] != "go test 全绿" {
		t.Fatalf("嵌入丢失 acceptance: %+v", embed.Goal.Acceptance)
	}
	if len(embed.Goal.Progress) == 0 || embed.Goal.Progress[0].Content != "框架装配完成" {
		t.Fatalf("嵌入丢失 progress: %+v", embed.Goal.Progress)
	}
	if len(embed.SessionTail) != 2 {
		t.Fatalf("会话尾窗应保留 2 条: %+v", embed.SessionTail)
	}
	if len(embed.Pending) == 0 {
		t.Fatal("嵌入应带待处理信号（compacted）")
	}
	if err := embed.Validate(); err != nil {
		t.Fatalf("嵌入不合法: %v", err)
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

// TestTLDisabledFallback 验证 TL 未启用（无评估器）时不评估、指令直连不可用。
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

// TestBadDirectiveRejected 验证 TL 输出非法/超长/漂移指令被拒（design §5.3 有界丢弃告警）。
func TestBadDirectiveRejected(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 超长内容。
	longReply := newStubEvaluator(TLDirective{Kind: DirectiveCorrect, Content: string(make([]rune, MaxDirectiveRunes+1))})
	sup := NewSupervisor(ctl, longReply, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup.RunEval(testCtx, "x"); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("超长指令应报 ErrBadDirective, 得 %v", err)
	}
	// 未知 kind。
	bogus := newStubEvaluator(TLDirective{Kind: "wat", Content: "x"})
	sup2 := NewSupervisor(ctl, bogus, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup2.RunEval(testCtx, "x"); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("非法 kind 应报 ErrBadDirective, 得 %v", err)
	}
	// goal 漂移。
	drift := newStubEvaluator(TLDirective{Kind: DirectiveCorrect, Content: "x", GoalID: "g-other"})
	sup3 := NewSupervisor(ctl, drift, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup3.RunEval(testCtx, "x"); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("goal 漂移应报 ErrBadDirective, 得 %v", err)
	}
}

// TestDirectiveEventAndRing 验证 controller.AppendDirective 发 goal.directive 事件并环形保留。
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
	// 事件流含 goal.directive。
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
