package conversation_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract"
)

func TestConversationTurnResponseFinalizesSuccessfulReply(t *testing.T) {
	history := []contract.EngineMessage{{Role: "assistant", Content: "reply"}}
	var calls []string
	response := applicationconversation.NewTurnResponse(applicationconversation.TurnResponseDeps{
		AttachReasoning: func(sessionID, requestID string) { calls = append(calls, "reasoning") },
		ObserveOutput: func(ctx context.Context, requestID, reply string, err error) error {
			calls = append(calls, "observe:"+reply)
			if err != nil {
				t.Fatalf("observe err = %v", err)
			}
			return errors.New("ignored observe error")
		},
		EnsureFinal: func(requestID, reply string) { calls = append(calls, "final:"+reply) },
		HistoryFor:  func(sessionID string) []contract.EngineMessage { return history },
		BackfillReasoning: func(sessionID string, got []contract.EngineMessage) {
			calls = append(calls, "backfill")
		},
	})

	response.Finalize(context.Background(), "session-a", "turn-a", "reply", nil)

	want := []string{"reasoning", "observe:reply", "final:reply", "backfill"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnResponseSkipsReasoningOnErrorAndEmptyFinal(t *testing.T) {
	turnErr := errors.New("provider failed")
	var calls []string
	response := applicationconversation.NewTurnResponse(applicationconversation.TurnResponseDeps{
		AttachReasoning: func(string, string) { calls = append(calls, "reasoning") },
		ObserveOutput: func(_ context.Context, _, reply string, err error) error {
			if !errors.Is(err, turnErr) {
				t.Fatalf("observe err = %v", err)
			}
			calls = append(calls, "observe")
			return nil
		},
		EnsureFinal: func(string, string) { calls = append(calls, "final") },
		HistoryFor:  func(string) []contract.EngineMessage { return nil },
		BackfillReasoning: func(string, []contract.EngineMessage) {
			calls = append(calls, "backfill")
		},
	})

	response.Finalize(context.Background(), "session-a", "turn-a", "", turnErr)

	want := []string{"observe", "backfill"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
