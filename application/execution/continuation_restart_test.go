package execution

import (
	"context"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	"github.com/RedHuang-0622/seelex/kernel/state"
	taskcontext "github.com/RedHuang-0622/seelex/kernel/task"
)

func restoredContinuationCoordinator(record model.SessionExecutionRecord) *taskcontext.Coordinator {
	coordinator := taskcontext.NewCoordinator(taskcontext.Deps{Core: &state.Core{}})
	coordinator.UpdateContinuationStateFor("session-a", func(next *model.SessionContinuationState) {
		next.StoppedByUser = record.StoppedByUser
		next.StoppedAt = record.StoppedAt
		next.Paused = record.Paused
		next.PausedAt = record.PausedAt
		next.PausedGoalID = record.PausedGoalID
		next.Continuations = append([]model.ContinuationAttempt(nil), record.Continuations...)
	})
	return coordinator
}

func restartContinuationHost(t *testing.T, coordinator *taskcontext.Coordinator) (*ContinuationApplication, *int) {
	t.Helper()
	submits := 0
	host := NewContinuationHost(ContinuationHostDeps{
		State: coordinator,
		Turns: &continuationHostTurns{
			task:      &model.TaskState{RequestID: "req-1", Status: dto.TurnInterrupted, UpdatedAt: time.Now()},
			requestID: "req-1",
		},
		Goals:       continuationHostGoals{view: activeGoal()},
		SessionIdle: func(string) bool { return true },
		Submit: func(context.Context, string, string) error {
			submits++
			return nil
		},
		Limits: func() limit.ContinuationLimits {
			return limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 3}
		},
	})
	return host, &submits
}

func TestRestartPreservesPauseBehavior(t *testing.T) {
	record := model.SessionExecutionRecord{Paused: true, PausedGoalID: "goal-1"}
	for attempt := 0; attempt < 2; attempt++ {
		host, submits := restartContinuationHost(t, restoredContinuationCoordinator(record))
		if pending := host.TriggerGoalContinuations("session-a", nil); pending != 0 || *submits != 0 {
			t.Fatalf("restart %d resumed a paused goal: pending=%d submits=%d", attempt, pending, *submits)
		}
	}
}

func TestRestartPreservesContinuationIdempotencyLedger(t *testing.T) {
	record := model.SessionExecutionRecord{Continuations: []model.ContinuationAttempt{{
		At: time.Now(), GoalID: "goal-1", RequestID: "req-1", Attempt: 1,
	}}}
	for attempt := 0; attempt < 2; attempt++ {
		host, submits := restartContinuationHost(t, restoredContinuationCoordinator(record))
		if pending := host.TriggerGoalContinuations("session-a", nil); pending != 0 || *submits != 0 {
			t.Fatalf("restart %d duplicated continuation for same request: pending=%d submits=%d",
				attempt, pending, *submits)
		}
	}
}
