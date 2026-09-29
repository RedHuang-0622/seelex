package goal

// round_lock_test.go — 回合三段式与回合闸门的**形状**判据（2026-09-29 锁面审计 §4.1）。
//
// 这组用例钉的不是"数值对不对"，而是"锁面形状对不对"：
//   - 执行段（模型调用 + 角色回合）不得持 s.mu：回合在飞时快照 / 其他入口必须立刻有响应；
//   - 回合闸门不可重入且**不排队**：第二个入口拿到 ErrRoundInFlight，而不是挂住；
//   - gate / 审批预筛遇到"已有回合在飞"走 B4 缺席默认（保持 active 转人工）；
//   - 回合内回头找 Supervisor 的回调不会自锁死（同 goroutine 重入非重入锁 = 死锁）；
//   - 回合期间 goal 被改 / 被收口的 B 语义：改 → 补 goal.update 帧；收口 → 丢弃结论。
//
// 每一个可能"挂住"的断言都带显式超时：形状错了必须**失败**，而不是把测试套件挂死。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/govern"
)

// roundShapeTimeout 是"形状判据"的统一超时：正常实现是微秒级返回，这里给足余量，
// 只有"被锁挡在门外"这种形状错误才会撞上它。
const roundShapeTimeout = 3 * time.Second

// gateEvaluator 是可控的评估器替身：hooks 模拟"回合执行期间发生的事"（回调 / 改 goal），
// entered 放行"已进入执行段"信号，release 决定这一轮何时返回。
type gateEvaluator struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	hooks   []func(ctx context.Context, embed TLSessionEmbed)
	reply   TLDirective
	err     error

	mu    sync.Mutex
	count int
}

// Evaluate 实现 TLEvaluator：hooks → 放行 entered → 等 release → 返回裁决。
// hooks 在 entered 之前跑，读断言因此不会与副作用赛跑。
func (e *gateEvaluator) Evaluate(ctx context.Context, embed TLSessionEmbed) (TLDirective, error) {
	e.mu.Lock()
	e.count++
	hooks := append([]func(context.Context, TLSessionEmbed){}, e.hooks...)
	reply, evalErr := e.reply, e.err
	e.mu.Unlock()

	for _, hook := range hooks {
		hook(ctx, embed)
	}
	if e.entered != nil {
		e.once.Do(func() { close(e.entered) })
	}
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return TLDirective{}, ctx.Err()
		case <-time.After(roundShapeTimeout):
			return TLDirective{}, errors.New("测试形状错误：评估器等待 release 超时")
		}
	}
	return reply, evalErr
}

func (e *gateEvaluator) evalCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// newBlockingEvaluator 构造"卡在执行段"的评估器（不 release 就一直不返回）。
func newBlockingEvaluator(reply TLDirective) *gateEvaluator {
	return &gateEvaluator{entered: make(chan struct{}), release: make(chan struct{}), reply: reply}
}

// newImmediateEvaluator 构造"立刻返回"的评估器，可带回合内副作用。
func newImmediateEvaluator(reply TLDirective, hooks ...func(context.Context, TLSessionEmbed)) *gateEvaluator {
	return &gateEvaluator{reply: reply, hooks: hooks}
}

func beginTestGoal(t *testing.T, ctl *Controller, title string) {
	t.Helper()
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: title}); err != nil {
		t.Fatalf("begin: %v", err)
	}
}

// waitSignal 等一个形状信号在超时内就绪。
func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(roundShapeTimeout):
		t.Fatalf("%s 超时未就绪（执行段可能仍持 s.mu）", what)
	}
}

// mustSnapshotWithin 断言快照能在超时内返回——即执行段**不持** s.mu。
func mustSnapshotWithin(t *testing.T, sup *Supervisor) TLState {
	t.Helper()
	done := make(chan TLState, 1)
	go func() { done <- sup.Snapshot() }()
	select {
	case state := <-done:
		return state
	case <-time.After(roundShapeTimeout):
		t.Fatal("回合在飞时 Snapshot 被挡在 s.mu 外超时未返回（执行段仍持锁）")
		return TLState{}
	}
}

// waitRound 收一个后台回合的结果（超时即失败，不挂死）。
func waitRound(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(roundShapeTimeout):
		t.Fatalf("%s 超时未结束", what)
		return nil
	}
}

// TestRoundDoesNotHoldSupervisorLockWhileEvaluating 钉住三段式的核心：**执行段不持
// s.mu**。旧实现（RunEval 锁住整轮）里这两条断言都会撞超时：快照读不到（被整轮挡住），
// in-flight 近端也读不到（放行时已被 clearInFlight 清空）。
func TestRoundDoesNotHoldSupervisorLockWhileEvaluating(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newBlockingEvaluator(TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"})
	evaluator.hooks = []func(context.Context, TLSessionEmbed){func(ctx context.Context, _ TLSessionEmbed) {
		if sink := TLDeltaSinkFrom(ctx); sink != nil {
			sink("正在写评审…")
		}
	}}
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	done := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "shape")
		done <- err
	}()
	waitSignal(t, evaluator.entered, "回合进入执行段")

	state := mustSnapshotWithin(t, sup)
	if state.Peer != PeerEvaluating {
		t.Fatalf("回合在飞时 peer 状态 = %q, want %q", state.Peer, PeerEvaluating)
	}
	if !strings.Contains(state.InFlight, "正在写评审") {
		t.Fatalf("回合在飞时应读得到 in-flight 近端（前端面板的可用性前提），得 %q", state.InFlight)
	}

	close(evaluator.release)
	if err := waitRound(t, done, "回合"); err != nil {
		t.Fatalf("回合应成功: %v", err)
	}
}

// TestRoundInFlightIsExplicitErrorNotQueue 钉住回合闸门的"不排队"纪律：第二个入口
// 拿到 ErrRoundInFlight（旧实现：排队等 s.mu，等于把调用方挂在一个可能永远不结束的
// 回合上）。
func TestRoundInFlightIsExplicitErrorNotQueue(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newBlockingEvaluator(TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"})
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	done := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "first")
		done <- err
	}()
	waitSignal(t, evaluator.entered, "第一个回合进入执行段")

	second := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "second")
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, ErrRoundInFlight) {
			t.Fatalf("第二个回合入口应得 ErrRoundInFlight, 得 %v", err)
		}
	case <-time.After(roundShapeTimeout):
		t.Fatal("第二个回合入口被排队挂住（应显式失败，不排队）")
	}

	close(evaluator.release)
	if err := waitRound(t, done, "第一个回合"); err != nil {
		t.Fatalf("第一个回合应成功: %v", err)
	}
	// 第一个回合结束后闸门必须重新可用（租约释放）。
	if _, err := sup.RunEval(testCtx, "third"); err != nil {
		t.Fatalf("回合结束后闸门应重新可用: %v", err)
	}
}

// TestProposeFinishWhileRoundInFlightEscalates 钉住 A 语义：终态 gate 遇到"已有回合
// 在飞"不排队，按 B4 缺席默认保持 active 转人工（a 永不等待 b）。
func TestProposeFinishWhileRoundInFlightEscalates(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newBlockingEvaluator(TLDirective{Kind: DirectiveVerdictDone, Content: "做完了"})
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	done := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "first")
		done <- err
	}()
	waitSignal(t, evaluator.entered, "回合进入执行段")

	type proposal struct {
		result FinishProposalResult
		err    error
	}
	proposed := make(chan proposal, 1)
	go func() {
		result, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "做完了"})
		proposed <- proposal{result: result, err: err}
	}()
	select {
	case got := <-proposed:
		if got.err != nil {
			t.Fatalf("在飞时 gate 不该报错（应按缺席默认升级）: %v", got.err)
		}
		if got.result.Outcome != OutcomeEscalate {
			t.Fatalf("在飞时 gate 结果 = %q, want %q", got.result.Outcome, OutcomeEscalate)
		}
		if !strings.Contains(got.result.Message, "进行中") {
			t.Fatalf("在飞时 gate 说明应点明『已有回合在进行中』, 得 %q", got.result.Message)
		}
	case <-time.After(roundShapeTimeout):
		t.Fatal("终态 gate 被排队挂住（应立刻按缺席默认升级）")
	}
	if _, ok := ctl.ActiveGoal(); !ok {
		t.Fatal("在飞时 goal 应保持 active")
	}

	close(evaluator.release)
	if err := waitRound(t, done, "在飞的回合"); err != nil {
		t.Fatalf("在飞的回合应成功: %v", err)
	}
}

// TestPreScreenApprovalWhileRoundInFlightEscalates 同一条 A 语义在审批预筛上的表现：
// 不排队，直接转人工（审批侧默认拒绝兜底不变）。
func TestPreScreenApprovalWhileRoundInFlightEscalates(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newBlockingEvaluator(TLDirective{Kind: DirectiveApprove, Content: "放行"})
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	done := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "first")
		done <- err
	}()
	waitSignal(t, evaluator.entered, "回合进入执行段")

	type screen struct {
		verdict ApprovalVerdict
		err     error
	}
	screened := make(chan screen, 1)
	go func() {
		verdict, err := sup.PreScreenApproval(testCtx, ApprovalScreenRequest{Summary: "删除目录", RiskLevel: "low"})
		screened <- screen{verdict: verdict, err: err}
	}()
	select {
	case got := <-screened:
		if got.err != nil {
			t.Fatalf("在飞时预筛不该报错: %v", got.err)
		}
		if got.verdict.Outcome != ApprovalOutcomeEscalate {
			t.Fatalf("在飞时预筛结果 = %q, want %q", got.verdict.Outcome, ApprovalOutcomeEscalate)
		}
	case <-time.After(roundShapeTimeout):
		t.Fatal("审批预筛被排队挂住（应立刻转人工）")
	}

	close(evaluator.release)
	if err := waitRound(t, done, "在飞的回合"); err != nil {
		t.Fatalf("在飞的回合应成功: %v", err)
	}
}

// TestInRoundCallbackDoesNotDeadlock 钉住"回合内回头找 Supervisor"的形状：同 goroutine
// 重入非重入锁 = 死锁。旧实现里这段回调（流式分片 / 迭代钩子 / 工具）撞上 s.mu 就会
// 永久挂住——这里是那条纪律的活体判据。
func TestInRoundCallbackDoesNotDeadlock(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	var (
		sup       *Supervisor
		notifyErr error
	)
	evaluator := newImmediateEvaluator(TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"},
		func(ctx context.Context, _ TLSessionEmbed) {
			notifyErr = sup.Notify(ctx, TLEvalSignal{
				Kind: SignalTurnCompleted, Source: "in_round", Detail: "回合内发生的执行动作",
			})
		})
	sup = NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	done := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "shape")
		done <- err
	}()
	if err := waitRound(t, done, "回合"); err != nil {
		t.Fatalf("回合应成功（回合内回调不得自锁死）: %v", err)
	}
	if notifyErr != nil {
		t.Fatalf("回合内 Notify 不该报错（已有回合在飞 = 本轮不评）: %v", notifyErr)
	}
	if got := evaluator.evalCount(); got != 1 {
		t.Fatalf("回合内 Notify 不应启动第二个回合, 评估次数 = %d", got)
	}

	// 事件**不丢**：回合内登记的那条进展会在下一次回合的准入段被抽成 work.progress
	// 帧（"登记照旧、评估留给下一次触发"这句注释的可观测判据）。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "next"}); err != nil {
		t.Fatalf("下一次触发应能评估: %v", err)
	}
	if got := evaluator.evalCount(); got != 2 {
		t.Fatalf("下一次触发应跑第二次回合, 评估次数 = %d", got)
	}
	found := false
	for _, frame := range sup.Snapshot().Frames {
		if frame.Kind == FrameWorkProgress && frame.Detail == "回合内发生的执行动作" {
			found = true
		}
	}
	if !found {
		t.Fatal("回合内登记的执行动作应在下一次回合的画面里（work.progress 帧）")
	}
}

// TestRoundDiscardedWhenGoalClosedDuringRound 钉住 B 语义之一：回合执行期间 goal 被
// **收口/取消** → 这一回合的结论无处落地，丢弃（不发 corr 信封、不落回合段、不计入
// 计数），并报可判定的 ErrRoundGoalGone。
func TestRoundDiscardedWhenGoalClosedDuringRound(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newImmediateEvaluator(TLDirective{Kind: DirectiveVerdictDone, Content: "做完了"},
		func(ctx context.Context, _ TLSessionEmbed) {
			if _, err := ctl.Finish(ctx, FinishRequest{Reason: "回合作答期间被取消"}); err != nil {
				t.Errorf("finish: %v", err)
			}
		})
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	_, err := sup.RunEval(testCtx, "shape")
	if !errors.Is(err, ErrRoundGoalGone) {
		t.Fatalf("goal 已收口时结论应丢弃并报 ErrRoundGoalGone, 得 %v", err)
	}
	if pending := sup.Mailbox().PendingDirectives(); pending != 0 {
		t.Fatalf("被丢弃的回合不应发布裁决, 待领取 %d 条", pending)
	}
	state := sup.Snapshot()
	if state.RoundCount != 0 || state.EvalCount != 0 {
		t.Fatalf("被丢弃的回合不应计入 b 回合/评估计数: rounds=%d evals=%d", state.RoundCount, state.EvalCount)
	}
}

// TestRoundEmitsGoalUpdateFrameWhenGoalChangedDuringRound 钉住 B 语义之二：回合执行
// 期间同一个 goal 被**改动**（那正是"只有 TL 能动 goal"这条权限要收口的场景）→
// 结论照常落地，并补一条 goal.update 差异帧，让 b 的下一轮基于新事实。
func TestRoundEmitsGoalUpdateFrameWhenGoalChangedDuringRound(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newImmediateEvaluator(TLDirective{Kind: DirectiveCheckpointOK, Content: "继续"},
		func(ctx context.Context, _ TLSessionEmbed) {
			if _, err := ctl.Update(ctx, UpdateRequest{
				ProgressKind: ProgressMilestone, ProgressContent: "回合期间的新进展",
			}); err != nil {
				t.Errorf("update: %v", err)
			}
		})
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	directive, err := sup.RunEval(testCtx, "shape")
	if err != nil {
		t.Fatalf("同一 goal 被改动不应丢弃这一回合: %v", err)
	}
	if directive.Kind != DirectiveCheckpointOK {
		t.Fatalf("裁决应原样落地, 得 %q", directive.Kind)
	}
	if pending := sup.Mailbox().PendingDirectives(); pending != 1 {
		t.Fatalf("裁决应照常发布, 待领取 %d 条", pending)
	}

	state := sup.Snapshot()
	found := false
	for _, frame := range state.Frames {
		if frame.Kind == FrameGoalUpdated && frame.Source == "goal_updated_during_round" {
			found = true
			if !strings.Contains(frame.Detail, "回合期间的新进展") {
				t.Fatalf("补的 goal.update 帧应带上新进度, 得 %q", frame.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("回合期间 goal 被改应补一条 goal.update 帧: %+v", state.Frames)
	}
}

// TestAdvisorSeatSkipsWhenRoundInFlight 钉住治理座位的处理口径：在飞是**良性跳过**
// （不 break、不报 roundError），而不是把"已有评审在跑"记成一次治理失败。
func TestAdvisorSeatSkipsWhenRoundInFlight(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	beginTestGoal(t, ctl, "目标")
	evaluator := newBlockingEvaluator(TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"})
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true})

	done := make(chan error, 1)
	go func() {
		_, err := sup.RunEval(testCtx, "first")
		done <- err
	}()
	waitSignal(t, evaluator.entered, "回合进入执行段")

	type seatTurn struct {
		action govern.TurnAction
		err    error
	}
	acted := make(chan seatTurn, 1)
	go func() {
		action, err := NewAdvisorSeat(sup, "advisor-b").Act(testCtx)
		acted <- seatTurn{action: action, err: err}
	}()
	select {
	case got := <-acted:
		if got.err != nil {
			t.Fatalf("在飞时座位发言应良性跳过，不该报治理失败: %v", got.err)
		}
		if got.action.BreakLoop {
			t.Fatal("跳过不应断环")
		}
		if !strings.Contains(got.action.Note, "进行中") {
			t.Fatalf("跳过应留下可读说明, 得 %q", got.action.Note)
		}
	case <-time.After(roundShapeTimeout):
		t.Fatal("治理座位被排队挂住（应良性跳过）")
	}

	close(evaluator.release)
	if err := waitRound(t, done, "在飞的回合"); err != nil {
		t.Fatalf("在飞的回合应成功: %v", err)
	}
}
