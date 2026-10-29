package execution

// continuation_host_test.go —— 续跑 gate 的**生产适配器**用例：两层状态（层一
// none/doing/done × 层二 continue/paused）与 stalled 时长都从真实事实源读出来，
// ⏸/⟳ 的写位也真的落到会话活状态上（docs/arch/goal-auto-continue.md §2.8.0/§2.8.1）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	"github.com/RedHuang-0622/seelex/kernel/state"
	taskcontext "github.com/RedHuang-0622/seelex/kernel/task"
)

const continuationHostSession = "session-a"

var continuationHostInterruptedAt = time.Unix(1_700_000_000, 0)

type continuationHostTurns struct {
	task      *model.TaskState
	requestID string
}

func (turns *continuationHostTurns) TaskStateFor(string) *model.TaskState { return turns.task }

func (turns *continuationHostTurns) CurrentRequestIDFor(string) string { return turns.requestID }

type continuationHostGoals struct {
	view dto.GoalStatusView
}

func (goals continuationHostGoals) StatusFor(string) (dto.GoalStatusView, error) {
	return goals.view, nil
}

type continuationHostFixture struct {
	states  *taskcontext.Coordinator
	turns   *continuationHostTurns
	host    *ContinuationApplication
	submits int
}

func newContinuationHostFixture(t *testing.T, view dto.GoalStatusView) *continuationHostFixture {
	t.Helper()
	fixture := &continuationHostFixture{
		states: taskcontext.NewCoordinator(taskcontext.Deps{Core: &state.Core{}}),
		turns: &continuationHostTurns{
			task: &model.TaskState{
				RequestID: "req-1", Status: dto.TurnInterrupted, UpdatedAt: continuationHostInterruptedAt,
			},
			requestID: "req-1",
		},
	}
	fixture.host = NewContinuationHost(ContinuationHostDeps{
		State:       fixture.states,
		Turns:       fixture.turns,
		Goals:       continuationHostGoals{view: view},
		SessionIdle: func(string) bool { return true },
		Submit: func(context.Context, string, string) error {
			fixture.submits++
			// 提交真的起了一个回合：当前 requestID 换新（gate 据此判"回合起来了"）。
			fixture.turns.requestID = fmt.Sprintf("req-cont-%d", fixture.submits)
			return nil
		},
		Limits: func() limit.ContinuationLimits {
			return limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 3}
		},
		Now: func() time.Time { return continuationHostInterruptedAt.Add(100 * time.Second) },
	})
	return fixture
}

func activeGoal() dto.GoalStatusView {
	return dto.GoalStatusView{
		Active: &dto.GoalRecord{ID: "goal-1", Title: "自动续跑", Status: dto.GoalActive},
	}
}

// 取值样例一：未收口 goal + 上一轮被硬中断 → doing/continue，且这条横条挂了 100 秒。
func TestContinuationHostReadsDoingContinueWithStalledDuration(t *testing.T) {
	fixture := newContinuationHostFixture(t, activeGoal())

	status := fixture.host.Status(continuationHostSession)
	if status.GoalStatus != dto.GoalRunDoing || status.Substate != dto.GoalSubstateContinue {
		t.Fatalf("两层状态 = %s/%s，期望 doing/continue", status.GoalStatus, status.Substate)
	}
	if !status.Stalled || status.Paused {
		t.Fatalf("横条状态 = stalled:%v paused:%v，期望 stalled 且未暂停", status.Stalled, status.Paused)
	}
	if status.StalledFor != 100*time.Second {
		t.Fatalf("stalled 时长 = %v，期望 100s", status.StalledFor)
	}
	view := status.View()
	if view == nil || view.StalledForSec != 100 || view.Status != dto.GoalRunDoing {
		t.Fatalf("横条投影 = %+v", view)
	}
}

// 取值样例二：⏸ 写位落到会话活状态（push/pull 共用同一个写点）→ doing/paused，横条收起。
func TestContinuationHostPauseWritesThroughToSessionState(t *testing.T) {
	fixture := newContinuationHostFixture(t, activeGoal())

	fixture.host.Pause(continuationHostSession)

	stored := fixture.states.ContinuationStateFor(continuationHostSession)
	if !stored.Paused || stored.PausedGoalID != "goal-1" {
		t.Fatalf("暂停位没有落到会话活状态：%+v", stored)
	}
	status := fixture.host.Status(continuationHostSession)
	if status.Substate != dto.GoalSubstatePaused || !status.Paused {
		t.Fatalf("层二 = %s，期望 paused", status.Substate)
	}
	if status.Stalled || status.StalledFor != 0 {
		t.Fatalf("暂停后横条不该还在等：%+v", status)
	}
	// 暂停只是"下一轮不再自动发"：真去扫门也不会起回合。
	if pending := fixture.host.TriggerGoalContinuations(continuationHostSession, nil); pending != 0 {
		t.Fatalf("暂停期间扫门返回 %d，期望 0", pending)
	}
	if fixture.submits != 0 {
		t.Fatalf("暂停期间起了 %d 个回合，期望 0", fixture.submits)
	}

	// ⟳/Continue 清位后回到 continue，扫门才会续一次。
	fixture.host.Continue(continuationHostSession)
	if status := fixture.host.Status(continuationHostSession); status.Substate != dto.GoalSubstateContinue {
		t.Fatalf("清位后层二 = %s，期望 continue", status.Substate)
	}
	fixture.host.TriggerGoalContinuations(continuationHostSession, nil)
	if fixture.submits != 1 {
		t.Fatalf("清位后续跑次数 = %d，期望 1", fixture.submits)
	}
	// ⑤ 幂等：同一中断点只续一次。
	if pending := fixture.host.TriggerGoalContinuations(continuationHostSession, map[string]struct{}{}); pending != 0 || fixture.submits != 1 {
		t.Fatalf("同一中断点续了第二次：pending=%d submits=%d", pending, fixture.submits)
	}
}

// 取值样例三：没有目标 = none（层二恒 continue）；目标已收口 = done（不再自动续）。
func TestContinuationHostReadsNoneAndDoneAsNotContinuable(t *testing.T) {
	none := newContinuationHostFixture(t, dto.GoalStatusView{})
	if status := none.host.Status(continuationHostSession); status.GoalStatus != dto.GoalRunNone || status.Substate != dto.GoalSubstateContinue {
		t.Fatalf("无目标 = %s/%s，期望 none/continue", status.GoalStatus, status.Substate)
	}
	if none.host.TriggerGoalContinuations(continuationHostSession, nil); none.submits != 0 {
		t.Fatalf("没有目标却续了 %d 次", none.submits)
	}

	done := newContinuationHostFixture(t, dto.GoalStatusView{History: 2})
	if status := done.host.Status(continuationHostSession); status.GoalStatus != dto.GoalRunDone {
		t.Fatalf("已收口 = %s，期望 done", status.GoalStatus)
	}
	if done.host.TriggerGoalContinuations(continuationHostSession, nil); done.submits != 0 {
		t.Fatalf("目标已收口却续了 %d 次", done.submits)
	}
}
