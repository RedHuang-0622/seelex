package conversation_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract"
)

func TestReActBudgetFinalizationInputHasSingleContractMarker(t *testing.T) {
	input := applicationconversation.ReactBudgetFinalizationInput
	if !strings.HasPrefix(input, contract.ReactBudgetFinalizationMarker) {
		t.Fatal("budget-finalization input must carry the contract marker")
	}
	for _, want := range []string{
		"Do not call any tools again",
		"summarize what was actually completed within the budget",
		"what remains unfinished",
		"Do not claim work that was not done",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("budget-finalization input missing %q: %s", want, input)
		}
	}
}

func TestTurnBudgetFinalizerPreservesPrepareStreamCleanupOrder(t *testing.T) {
	var calls []string
	finalizer := applicationconversation.NewTurnBudgetFinalizer(applicationconversation.TurnBudgetFinalizerDeps{
		SessionIDFromContext: func(context.Context) string { return "session-a" },
		BudgetError:          func(string) error { return contract.ErrReActBudgetExceeded },
		WrapError:            func(err error) error { return err },
		PrepareContext: func(sessionID, requestID, input string) (string, error) {
			calls = append(calls, "prepare:"+sessionID+":"+requestID)
			if input != applicationconversation.ReactBudgetFinalizationInput {
				t.Fatalf("prepare input = %q", input)
			}
			return "prepared", nil
		},
		Stream: func(_ context.Context, sessionID, input string, _ func(string)) (string, error) {
			calls = append(calls, "stream:"+sessionID+":"+input)
			return "summary", nil
		},
		RemoveInjectedInput: func(sessionID string) error {
			calls = append(calls, "cleanup:"+sessionID)
			return nil
		},
	})

	if err := finalizer.Finalize(context.Background(), "turn-a", func(string) {}); err != nil {
		t.Fatal(err)
	}
	want := "prepare:session-a:turn-a,stream:session-a:prepared,cleanup:session-a"
	if got := strings.Join(calls, ","); got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestTurnBudgetFinalizerReturnsCleanupFailureAfterDelivery(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	finalizer := applicationconversation.NewTurnBudgetFinalizer(applicationconversation.TurnBudgetFinalizerDeps{
		SessionIDFromContext: func(context.Context) string { return "session-a" },
		BudgetError:          func(string) error { return contract.ErrReActBudgetExceeded },
		WrapError:            func(err error) error { return err },
		PrepareContext:       func(string, string, string) (string, error) { return "prepared", nil },
		Stream:               func(context.Context, string, string, func(string)) (string, error) { return "summary", nil },
		RemoveInjectedInput:  func(string) error { return cleanupErr },
	})

	err := finalizer.Finalize(context.Background(), "turn-a", func(string) {})
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("Finalize error = %v, want cleanup failure", err)
	}
}
