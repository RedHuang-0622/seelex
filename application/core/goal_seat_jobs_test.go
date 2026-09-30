package core

// goal_seat_jobs_test.go — 钉住 M2 的最后一块：goal 座位循环降级为
// jobs.KindSeat 执行体（D4：它不再是"与 jobs 并列的第二套驱动"）。
//
// 三条契约在这里钉住：
//   - 装配了座位作业面时，AdvanceAfterChat 走**派发 + 有界汇合**（载荷带会话归属与
//     本轮正文：作业的执行 ctx 是作业面从 Background 派生的，带不了这两件事）；
//   - 汇合返回时（即 AdvanceAfterChat 返回后）裁决/轮次已在本回合可见 —— 与同步
//     路径同一个可见性契约，不因驱动换成作业而晚一轮；
//   - 作业非 done（failed）⇒ 失败原因走**同一条**登记路径进
//     GoalGovernanceView.RoundError；未装配作业面（SeatJobs == nil）⇒ 现状同步循环。
//
// "驱动唯一化"也在这里钉：假执行体跑的就是 runSeatRound —— 作业路径与同步降级路径
// 共用同一份循环正文（不存在"同步一份 + 作业里再一份"）。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// fakeSeatDispatch 是一次派发收到的载荷。
type fakeSeatDispatch struct {
	sessionID string
	detail    string
}

// fakeSeatJobs 是座位作业面的桩：DispatchSeat 立即返回受理回执，循环在**另一个
// goroutine** 里跑（像一个真作业），JoinSeat 才把它等完。这样"裁决在
// AdvanceAfterChat 返回时已可见"这条断言才真的钉住汇合语义——若派发里同步跑完，
// 它会被蒙对。
type fakeSeatJobs struct {
	executor func(ctx context.Context, sessionID, detail string) error

	mu         sync.Mutex
	dispatched []fakeSeatDispatch
	joins      []string
	done       chan struct{}
	outcome    dto.SeatJobOutcome
}

func (jobs *fakeSeatJobs) DispatchSeat(ctx context.Context, sessionID, detail string) (string, error) {
	jobs.mu.Lock()
	jobs.dispatched = append(jobs.dispatched, fakeSeatDispatch{sessionID: sessionID, detail: detail})
	executor := jobs.executor
	done := make(chan struct{})
	jobs.done = done
	jobs.mu.Unlock()
	// 作业的执行面从 Background 派生（真作业面就是这样）：执行体拿不到调用方的
	// ctx，因此会话归属与正文只能来自载荷。
	go func() {
		defer close(done)
		if executor != nil {
			_ = executor(context.Background(), sessionID, detail)
		}
	}()
	return "seat-1", nil
}

func (jobs *fakeSeatJobs) JoinSeat(ctx context.Context, handle string, budget time.Duration) (dto.SeatJobOutcome, error) {
	jobs.mu.Lock()
	done := jobs.done
	jobs.joins = append(jobs.joins, handle)
	jobs.mu.Unlock()
	if done == nil {
		return dto.SeatJobOutcome{}, errors.New("座位作业未派发")
	}
	select {
	case <-done:
	case <-ctx.Done():
		return dto.SeatJobOutcome{}, ctx.Err()
	case <-time.After(budget):
		return dto.SeatJobOutcome{}, errors.New("座位作业汇合超时")
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	outcome := jobs.outcome
	if outcome.State == "" {
		outcome.State = seatJobStateDone
		outcome.Known = true
	}
	return outcome, nil
}

func (jobs *fakeSeatJobs) recorded() []fakeSeatDispatch {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return append([]fakeSeatDispatch(nil), jobs.dispatched...)
}

func (jobs *fakeSeatJobs) joined() []string {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return append([]string(nil), jobs.joins...)
}

// TestAdvanceAfterChatDispatchesSeatJob：作业路径——派发（载荷带会话与正文）→ 有界
// 汇合 → 裁决/轮次在 AdvanceAfterChat 返回时已可见。
func TestAdvanceAfterChatDispatchesSeatJob(t *testing.T) {
	seatJobs := &fakeSeatJobs{}
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{{
			Kind: goaldomain.DirectiveCorrect, Content: "先补负路径单测再收口",
		}}},
		SeatJobs: seatJobs,
	})
	// 假执行体跑的是座位循环**同一份正文**（runSeatRound）——这就是驱动唯一化的接线：
	// 作业里的循环与同步降级路径的循环不是两份实现。
	seatJobs.executor = func(ctx context.Context, sessionID, detail string) error {
		return coordinator.runSeatRound(ctx, sessionID, detail)
	}

	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "sess-seat", goaldomain.BeginRequest{Title: "座位作业"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "sess-seat", "本轮工作正文"); err != nil {
		t.Fatalf("advance after chat: %v", err)
	}

	dispatched := seatJobs.recorded()
	if len(dispatched) != 1 {
		t.Fatalf("应派发一个座位作业，得到 %d 次: %+v", len(dispatched), dispatched)
	}
	if dispatched[0].sessionID != "sess-seat" || dispatched[0].detail != "本轮工作正文" {
		t.Fatalf("派发载荷必须带会话归属与本轮正文（作业 ctx 里没有它们）: %+v", dispatched[0])
	}
	if joined := seatJobs.joined(); len(joined) != 1 || joined[0] != "seat-1" {
		t.Fatalf("派发出去的句柄应被有界汇合: %+v", joined)
	}

	// 裁决本回合可见：AdvanceAfterChat 返回即已产出（不是"下一轮才看到"）。
	directives := coordinator.PeekDirectives("sess-seat")
	if len(directives) != 1 || directives[0].Content != "先补负路径单测再收口" {
		t.Fatalf("裁决应在产出它的那一回合就可见: %+v", directives)
	}
	view := coordinator.GoalGovernanceViewFor("sess-seat")
	if view == nil || view.Round < 1 {
		t.Fatalf("座位作业必须真的把一轮治理跑完（Round≥1）: %+v", view)
	}
	if view.RoundError != "" {
		t.Fatalf("成功汇合不应留下失败记录: %+v", view)
	}
}

// TestAdvanceAfterChatSeatJobFailureLandsInRoundError：作业非 done ⇒ 本轮失败原因
// 进只读视图（与 gov.Next 报错同一条登记路径），goal 保持 active（安全默认）。
func TestAdvanceAfterChatSeatJobFailureLandsInRoundError(t *testing.T) {
	seatJobs := &fakeSeatJobs{outcome: dto.SeatJobOutcome{
		State: "failed", ExitCode: 1, Summary: "座位循环失败：ADVISOR 回合不可用", Known: true,
	}}
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{},
		SeatJobs:  seatJobs,
	})
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "sess-seat-fail", goaldomain.BeginRequest{Title: "座位作业失败"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "sess-seat-fail", "本轮工作正文"); err == nil {
		t.Fatal("座位作业非 done 应把失败原因返回给调用方")
	}
	view := coordinator.GoalGovernanceViewFor("sess-seat-fail")
	if view == nil || !view.Active {
		t.Fatalf("作业失败不应改变 goal 的 active 态: %+v", view)
	}
	if view.RoundError != "座位循环失败：ADVISOR 回合不可用" {
		t.Fatalf("作业失败原因应登记进 RoundError: %+v", view)
	}
}

// TestSeatJobsNilKeepsSynchronousLoop：未装配座位作业面（桩宿主 / 未接线宿主）⇒
// 现状同步循环：没有派发、循环原地跑完，裁决同样本回合可见。这就是"未装配时行为
// 一字不变"这条约束的显式钉子（现有 goal_* 用例覆盖同一形态）。
func TestSeatJobsNilKeepsSynchronousLoop(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{{
			Kind: goaldomain.DirectiveCheckpointOK, Content: "继续",
		}}},
	})
	if coordinator.deps.SeatJobs != nil {
		t.Fatal("该用例的前提是座位作业面为 nil（未接线宿主）")
	}
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "sess-sync", goaldomain.BeginRequest{Title: "同步降落"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "sess-sync", "本轮工作正文"); err != nil {
		t.Fatalf("同步路径不应报错: %v", err)
	}
	view := coordinator.GoalGovernanceViewFor("sess-sync")
	if view == nil || view.Round < 1 || view.RoundError != "" {
		t.Fatalf("未装配作业面时应原地同步跑完一轮: %+v", view)
	}
}

// TestRunSeatRoundExecutesSharedLoop：执行侧入口（Service.RunSeatRound，组合根经
// Runtime.SetSeatRoundRunner 注入）跑的就是那一份 runSeatRound，并把可读结论经
// note 交给作业输出面。
func TestRunSeatRoundExecutesSharedLoop(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	service.SetGoalTLEvaluator(&capturingTLEvaluator{})

	sessionID := "sess-seat-round"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{Title: "执行侧入口"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}

	var notes []string
	if err := service.RunSeatRound(context.Background(), sessionID, "本轮工作正文", func(text string) {
		notes = append(notes, text)
	}); err != nil {
		t.Fatalf("RunSeatRound: %v", err)
	}
	view := service.GoalGovernanceViewFor(sessionID)
	if view == nil || view.Round < 1 {
		t.Fatalf("执行侧入口必须真的推进一轮治理: %+v", view)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "round=") {
		t.Fatalf("作业输出面应收到一行可读结论: %+v", notes)
	}

	// 空会话归属 = 显式报错（作业载荷丢了 session_id 时不能静默跑在别人会话上）。
	if err := service.RunSeatRound(context.Background(), "  ", "正文", nil); err == nil {
		t.Fatal("缺会话归属应显式报错")
	}
}
