package conversation

// turn_runner_continuation_test.go —— 自动续跑的**信号点**用例（硬判据：一个含 N 个
// 工具轮的回合只判定 1 次，不是 N+1）。
//
// 权威语义在 docs/arch/goal-auto-continue.md §2.8.2：回合收尾漏斗只有
// turn_runner.Run 一处，信号必须在**回合休止点**（Commit + Finalize 之后）发一次；
// 发在 loop / 工具轮上就是 N 次唤醒 × 每轮一次真金白银的回合。
//
// 用例把信号接到**真的续跑 gate**（application/execution）上，因此断言的是"判定
// 发生了几次"而不是"某个函数被调用了几次"：gate 判 1 次、起回合 1 次。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/execution"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	session_runtime "github.com/RedHuang-0622/seelex/kernel/session"
)

// continuationSignalHarness 记录"回合里发生了多少次工具轮"与"判定/起回合各几次"。
type continuationSignalHarness struct {
	mu         sync.Mutex
	order      []string
	toolRounds int
	signals    int
	judgments  int
	submits    int
}

func (h *continuationSignalHarness) record(step string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.order = append(h.order, step)
}

// gate 是**真的**续跑 gate：事实源给的是一份"未收口 goal + 上一轮被硬中断"的现场。
func (h *continuationSignalHarness) gate() *execution.ContinuationApplication {
	return execution.NewContinuation(execution.ContinuationDeps{
		Facts: func(string) execution.ContinuationFacts {
			return execution.ContinuationFacts{
				GoalID:        "goal-1",
				GoalStatus:    dto.GoalRunDoing,
				GoalTitle:     "回合休止点只判一次",
				TurnStatus:    dto.TurnInterrupted,
				RequestID:     "req-1",
				Continued:     map[string]bool{},
				Truncated:     map[string]bool{},
				InterruptedAt: time.Unix(1_700_000_000, 0),
			}
		},
		SessionIdle: func(string) bool { return true },
		SubmitConversation: func(context.Context, string, string) error {
			h.mu.Lock()
			h.submits++
			h.mu.Unlock()
			h.record("submit")
			return nil
		},
		Limits: func() limit.ContinuationLimits {
			return limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 3}
		},
		Now: func() time.Time { return time.Unix(1_700_000_100, 0) },
	})
}

// runner 组装一条最小但完整的回合链：Stream 里跑 N 个工具轮，收尾走 Commit/Finalize。
func (h *continuationSignalHarness) runner(gate *execution.ContinuationApplication) *TurnRunnerApplication {
	mu := &sync.Mutex{}
	batcher := &testBatcher{}
	return NewTurnRunner(TurnRunnerDeps{
		WithSessionID:      func(ctx context.Context, _ string) context.Context { return ctx },
		StartDraftSnapshot: func(context.Context, string, string) func() { return func() {} },
		PrepareExecution:   func(string) {},
		ClearReActBudget:   func(string) {},
		Debug:              func(string, ...any) {},
		NewBatchedDeltaSink: func(string) (StreamBatcher, func(string)) {
			return batcher, func(string) {}
		},
		Stream: func(context.Context, string, string, func(string)) (string, error) {
			// 这一轮里跑 N 个工具轮：每个工具轮都是一次 loop 迭代，但**不是**回合休止点。
			for round := 0; round < 3; round++ {
				h.mu.Lock()
				h.toolRounds++
				h.mu.Unlock()
				h.record("tool-round")
			}
			return "reply", nil
		},
		Input: NewTurnInput(TurnInputDeps{
			ApplyPrompt:    func(string, string) {},
			PrepareContext: func(_, _, input string) (string, error) { return input, nil },
			Normalize:      func(input string) string { return input },
		}),
		Response: NewTurnResponse(TurnResponseDeps{
			AttachReasoning:   func(string, string) {},
			ObserveOutput:     func(context.Context, string, string, error) error { return nil },
			EnsureFinal:       func(string, string) {},
			HistoryFor:        func(string) []contract.EngineMessage { return nil },
			BackfillReasoning: func(string, []contract.EngineMessage) {},
		}),
		Recovery: NewTurnRecovery(TurnRecoveryDeps{
			TakeContextFailure: func(string) error { return nil },
			Recover:            func(context.Context, error, string) (bool, error) { return false, nil },
			Retryable:          func(error) bool { return false },
			FinalizeBudget:     func(context.Context, string, func(string)) error { return nil },
			FinalizeTask:       func(context.Context, string, string) error { return nil },
		}),
		Cleanup: NewTurnCleanup(TurnCleanupDeps{
			StampRoute:        func(string, string) {},
			DiscardMailbox:    func(string) {},
			CollectProjection: func(string) view_state.RuntimeStateProjection { return view_state.RuntimeStateProjection{} },
			RemoveCheckpoints: func(string) error { return nil },
			RemoveRecovery:    func(string) error { return nil },
			RecordError:       func(string, string, error) {},
		}),
		Commit: NewTurnCommit(TurnCommitDeps{
			Persist: func(string, string, error) (session_runtime.Location, error) {
				h.record("commit")
				return session_runtime.Location{}, nil
			},
			Lock:     func() sync.Locker { return mu },
			Unit:     func(string) *session.SessionUnit { return nil },
			IsActive: func(string) bool { return true },
			ApplyOutcome: func(string, string, *session.SessionUnit, view_state.RuntimeStateProjection, error) (string, bool) {
				return "", false
			},
			Promote:    func(string, *session.SessionUnit) Promotion { return Promotion{} },
			AppendNext: func(string, bool, Promotion) (*model.Message, *model.Message) { return nil, nil },
			Mirror:     func(string, *session.SessionUnit, bool) uint64 { return 1 },
			Stale:      func(string, string, *session.SessionUnit) {},
		}),
		Finalize: NewTurnFinalize(TurnFinalizeDeps{
			TailComplete: func(session_runtime.Location, Publication, context.Context) {
				h.record("finalize")
			},
		}),
		// 回合休止点的唯一唤醒口：这里就是"判定"发生的地方（真 gate 扫一次门）。
		SignalContinuation: func(sessionID string) {
			h.mu.Lock()
			h.signals++
			h.mu.Unlock()
			h.record("signal")
			h.mu.Lock()
			h.judgments++
			h.mu.Unlock()
			gate.TriggerGoalContinuations(sessionID, nil)
		},
	})
}

// 硬判据：一个含 3 个工具轮的回合 → 信号 1 次、判定 1 次、起回合 1 次（不是 4 次）。
func TestContinuationSignalFiresOncePerTurnNotPerToolRound(t *testing.T) {
	harness := &continuationSignalHarness{}
	harness.runner(harness.gate()).Run(
		context.Background(), "session-a", "req-1", Request{DisplayInput: "go on", ModelInput: "go on"},
	)

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if harness.toolRounds != 3 {
		t.Fatalf("场景不成立：工具轮 = %d，期望 3", harness.toolRounds)
	}
	if harness.signals != 1 {
		t.Fatalf("回合休止点信号 = %d，期望 1（不是每个工具轮一次，那会是 %d）", harness.signals, harness.toolRounds+1)
	}
	if harness.judgments != 1 {
		t.Fatalf("续跑判定 = %d，期望 1", harness.judgments)
	}
	if harness.submits != 1 {
		t.Fatalf("续跑起回合 = %d，期望 1", harness.submits)
	}
	// 信号必须在回合收尾之后（Commit 与 Finalize 都完成）：休止点，不是轮中。
	var commitIndex, signalIndex = -1, -1
	for index, step := range harness.order {
		switch step {
		case "commit":
			commitIndex = index
		case "signal":
			signalIndex = index
		}
	}
	if commitIndex < 0 || signalIndex < 0 || signalIndex < commitIndex {
		t.Fatalf("信号不是回合休止点发的：order = %v", harness.order)
	}
}
