package conversation_test

import (
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
)

func TestConversationTurnInputPreparesPromptAndContext(t *testing.T) {
	var calls []string
	input := applicationconversation.NewTurnInput(applicationconversation.TurnInputDeps{
		ApplyPrompt: func(sessionID, requestID string) {
			calls = append(calls, "prompt:"+sessionID+":"+requestID)
		},
		PrepareContext: func(sessionID, requestID, modelInput string) (string, error) {
			calls = append(calls, "context:"+modelInput)
			return "prepared", nil
		},
		Normalize: func(value string) string {
			calls = append(calls, "normalize:"+value)
			return value + "-normalized"
		},
	})

	got, err := input.Prepare("session-a", "turn-a", "input")

	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got != "prepared-normalized" {
		t.Fatalf("input = %q", got)
	}
	if got := strings.Join(calls, ","); got != "prompt:session-a:turn-a,context:input,normalize:prepared" {
		t.Fatalf("calls = %q", got)
	}
}

func TestConversationTurnInputStopsOnContextError(t *testing.T) {
	prepareErr := errors.New("context failed")
	input := applicationconversation.NewTurnInput(applicationconversation.TurnInputDeps{
		ApplyPrompt: func(string, string) {},
		PrepareContext: func(string, string, string) (string, error) {
			return "", prepareErr
		},
		Normalize: func(string) string {
			t.Fatal("context 失败后不得归一化输入")
			return ""
		},
	})

	_, err := input.Prepare("session-a", "turn-a", "input")

	if !errors.Is(err, prepareErr) {
		t.Fatalf("Prepare error = %v, want context error", err)
	}
}
