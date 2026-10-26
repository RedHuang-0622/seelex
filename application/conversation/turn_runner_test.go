package conversation

import (
	"context"
	"sync"
	"testing"

	session "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type testBatcher struct{ flushed bool }

func (batcher *testBatcher) Flush() error {
	batcher.flushed = true
	return nil
}

func TestTurnRunnerExecutesComponentOrder(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	batcher := &testBatcher{}
	runner := NewTurnRunner(TurnRunnerDeps{
		WithSessionID: func(ctx context.Context, sessionID string) context.Context {
			return ctx
		},
		StartDraftSnapshot: func(context.Context, string, string) func() {
			calls = append(calls, "draft")
			return func() { calls = append(calls, "draft-stop") }
		},
		PrepareExecution: func(string) { calls = append(calls, "prepare") },
		ClearReActBudget: func(string) { calls = append(calls, "budget-clear") },
		Debug:            func(string, ...any) {},
		NewBatchedDeltaSink: func(string) (StreamBatcher, func(string)) {
			return batcher, func(string) {}
		},
		Stream: func(context.Context, string, string, func(string)) (string, error) {
			calls = append(calls, "stream")
			return "reply", nil
		},
		Input: NewTurnInput(TurnInputDeps{
			ApplyPrompt: func(string, string) { calls = append(calls, "prompt") },
			PrepareContext: func(_, _, input string) (string, error) {
				calls = append(calls, "context")
				return input, nil
			},
			Normalize: func(input string) string { return input },
		}),
		Response: NewTurnResponse(TurnResponseDeps{
			AttachReasoning: func(string, string) { calls = append(calls, "reasoning") },
			ObserveOutput: func(context.Context, string, string, error) error {
				calls = append(calls, "observe")
				return nil
			},
			EnsureFinal: func(string, string) { calls = append(calls, "final-message") },
			HistoryFor:  func(string) []contract.EngineMessage { return nil },
			BackfillReasoning: func(string, []contract.EngineMessage) {
				calls = append(calls, "backfill")
			},
		}),
		Recovery: NewTurnRecovery(TurnRecoveryDeps{
			TakeContextFailure: func(string) error { return nil },
			Recover:            func(context.Context, error, string) (bool, error) { return false, nil },
			Retryable:          func(error) bool { return false },
			FinalizeBudget:     func(context.Context, string, func(string)) error { return nil },
			FinalizeTask:       func(context.Context, string, string) error { return nil },
		}),
		Cleanup: NewTurnCleanup(TurnCleanupDeps{
			StampRoute:        func(string, string) { calls = append(calls, "route") },
			DiscardMailbox:    func(string) { calls = append(calls, "mailbox") },
			CollectProjection: func(string) view_state.RuntimeStateProjection { return view_state.RuntimeStateProjection{} },
			RemoveCheckpoints: func(string) error { return nil },
			RemoveRecovery:    func(string) error { return nil },
			RecordError:       func(string, string, error) {},
		}),
		Commit: NewTurnCommit(TurnCommitDeps{
			Persist: func(string, string, error) (session_runtime.Location, error) {
				calls = append(calls, "persist")
				return session_runtime.Location{}, nil
			},
			Lock:     func() sync.Locker { return &mu },
			Unit:     func(string) *session.SessionUnit { return nil },
			IsActive: func(string) bool { return true },
			ApplyOutcome: func(string, string, *session.SessionUnit, view_state.RuntimeStateProjection, error) (string, bool) {
				calls = append(calls, "outcome")
				return "", false
			},
			Promote: func(string, *session.SessionUnit) Promotion { return Promotion{} },
			AppendNext: func(string, bool, Promotion) (*model.Message, *model.Message) {
				return nil, nil
			},
			Mirror: func(string, *session.SessionUnit, bool) uint64 {
				calls = append(calls, "mirror")
				return 1
			},
			Stale: func(string, string, *session.SessionUnit) {},
		}),
		Finalize: NewTurnFinalize(TurnFinalizeDeps{
			TailComplete: func(session_runtime.Location, Publication, context.Context) {
				calls = append(calls, "tail")
			},
		}),
	})

	runner.Run(context.Background(), "s1", "r1", Request{DisplayInput: "hello", ModelInput: "hello"})
	if !batcher.flushed {
		t.Fatal("runner did not flush the stream batcher")
	}
	if len(calls) == 0 || calls[0] != "draft" {
		t.Fatalf("runner order = %v", calls)
	}
}
