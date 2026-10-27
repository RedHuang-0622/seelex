package conversation_test

import (
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
)

func TestConversationTurnExecutionPreparesInOrder(t *testing.T) {
	var calls []string
	execution := applicationconversation.NewTurnExecution(applicationconversation.TurnExecutionDeps{
		BindProjectRoot:     func(sessionID string) { calls = append(calls, "root") },
		SyncSubagentPolicy:  func(sessionID string) { calls = append(calls, "subagent") },
		SyncReasoningEffort: func(sessionID string) { calls = append(calls, "reasoning") },
		SyncFullAccess:      func(sessionID string) { calls = append(calls, "full-access") },
	})

	execution.Prepare("session-a")

	want := "root,subagent,reasoning,full-access"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}
