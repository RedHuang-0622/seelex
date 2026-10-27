package conversation_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
)

func TestConversationTurnRecoveryRetriesAfterRecovery(t *testing.T) {
	turnErr := errors.New("context overflow")
	var calls []string
	recovery := applicationconversation.NewTurnRecovery(applicationconversation.TurnRecoveryDeps{
		TakeContextFailure: func(requestID string) error { return turnErr },
		Recover: func(ctx context.Context, gotErr error, input string) (bool, error) {
			calls = append(calls, "recover:"+input)
			if !errors.Is(gotErr, turnErr) {
				t.Fatalf("recover err = %v", gotErr)
			}
			return true, nil
		},
		Retryable: func(err error) bool {
			calls = append(calls, "retryable")
			return true
		},
		Retry: func(ctx context.Context, requestID string, onChunk func(string)) error {
			calls = append(calls, "retry")
			return nil
		},
		FinalizeBudget: func(context.Context, string, func(string)) error {
			calls = append(calls, "budget")
			return nil
		},
		FinalizeTask: func(ctx context.Context, requestID, reply string) error {
			calls = append(calls, "task:"+reply)
			return nil
		},
	})

	err := recovery.Finalize(context.Background(), "session-a", "turn-a", "input", "reply", nil, func(string) {})

	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	want := []string{"recover:input", "retryable", "retry", "budget", "task:reply"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnRecoveryPropagatesRecoveryAndRetryErrors(t *testing.T) {
	baseErr := errors.New("base")
	recoveryErr := errors.New("recovery")
	retryErr := errors.New("retry")
	tests := []struct {
		name        string
		recoveryErr error
		retryErr    error
		text        string
		budget      bool
	}{
		{name: "recovery", recoveryErr: recoveryErr, text: "context recovery failed"},
		{name: "retry", retryErr: retryErr, text: "bounded context recovery turn failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			budgetCalled := false
			recovery := applicationconversation.NewTurnRecovery(applicationconversation.TurnRecoveryDeps{
				TakeContextFailure: func(string) error { return nil },
				Recover: func(context.Context, error, string) (bool, error) {
					if test.recoveryErr != nil {
						return false, test.recoveryErr
					}
					return true, nil
				},
				Retryable: func(error) bool { return true },
				Retry:     func(context.Context, string, func(string)) error { return test.retryErr },
				FinalizeBudget: func(context.Context, string, func(string)) error {
					budgetCalled = true
					return nil
				},
				FinalizeTask: func(context.Context, string, string) error { return nil },
			})

			err := recovery.Finalize(context.Background(), "session-a", "turn-a", "input", "reply", baseErr, func(string) {})

			if !errors.Is(err, baseErr) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Finalize error = %v", err)
			}
			if budgetCalled {
				t.Fatal("恢复失败后不得进入 budget finalization")
			}
		})
	}
}

func TestConversationTurnRecoveryStopsBeforeTaskOnBudgetError(t *testing.T) {
	budgetErr := errors.New("budget failed")
	taskCalled := false
	recovery := applicationconversation.NewTurnRecovery(applicationconversation.TurnRecoveryDeps{
		TakeContextFailure: func(string) error { return nil },
		Recover:            func(context.Context, error, string) (bool, error) { return false, nil },
		Retryable:          func(error) bool { return false },
		Retry:              func(context.Context, string, func(string)) error { return nil },
		FinalizeBudget:     func(context.Context, string, func(string)) error { return budgetErr },
		FinalizeTask: func(context.Context, string, string) error {
			taskCalled = true
			return nil
		},
	})

	err := recovery.Finalize(context.Background(), "session-a", "turn-a", "input", "reply", nil, func(string) {})

	if !errors.Is(err, budgetErr) || taskCalled {
		t.Fatalf("Finalize error = %v taskCalled=%v", err, taskCalled)
	}
}
