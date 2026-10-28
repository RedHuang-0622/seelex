package conversation_test

import (
	"context"
	"errors"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestTurnErrorApplicationClassifiesUnhandledTaskFailures(t *testing.T) {
	providerErr := errors.New("provider disconnected")
	tests := []struct {
		name   string
		err    error
		status model.TurnStatus
		skip   bool
		want   string
	}{
		{name: "canceled", status: model.TurnProgressing, err: context.Canceled, want: "interrupt:The task was canceled before completion."},
		{name: "provider", status: model.TurnProgressing, err: providerErr, want: "interrupt:The provider interrupted the task before completion."},
		{name: "unverified", status: model.TurnProgressing, err: errors.New("stopped"), want: "fail"},
		{name: "terminal", status: model.TurnCompleted, err: errors.New("late"), skip: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			application := applicationconversation.NewTurnErrorApplication(applicationconversation.TurnErrorDeps{
				TaskFor: func(string) *model.TaskState {
					return &model.TaskState{RequestID: "turn-a", Status: test.status}
				},
				ClassifyProviderFailure: func(error) contract.ProviderFailureKind {
					if errors.Is(test.err, providerErr) {
						return contract.ProviderFailureTimeout
					}
					return contract.ProviderFailureNone
				},
				Interrupt: func(_ string, reason string) { calls = append(calls, "interrupt:"+reason) },
				Fail:      func(string, string) { calls = append(calls, "fail") },
			})

			application.Record("session-a", "turn-a", test.err)

			if test.skip {
				if len(calls) != 0 {
					t.Fatalf("calls = %v, want none", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0] != test.want {
				t.Fatalf("calls = %v, want %q", calls, test.want)
			}
		})
	}
}
