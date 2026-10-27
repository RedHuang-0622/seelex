package conversation_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/kernel/session"
)

func TestConversationTurnTailCompletesWithoutPromotion(t *testing.T) {
	var calls []string
	tail := applicationconversation.NewTurnTail(applicationconversation.TurnTailDeps{
		RebindWorkspaceWhenIdle: func() { calls = append(calls, "rebind") },
		MarkPromotedConsumed: func(session_runtime.Location, string, string) error {
			t.Fatal("无队列提升时不得标记 consumed")
			return nil
		},
		Publish: func(applicationconversation.Publication) { calls = append(calls, "publish") },
		StartNext: func(context.Context, string, string, applicationconversation.Request) {
			t.Fatal("无队列提升时不得启动下一轮")
		},
		ReconcileSessionRelease: func() { calls = append(calls, "release") },
		FlushToolRecord:         func(string) { calls = append(calls, "flush") },
	})

	tail.Complete(session_runtime.Location{}, applicationconversation.Publication{
		SessionID: "session-a",
		RequestID: "turn-a",
	}, nil)

	want := []string{"rebind", "publish", "release", "flush"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnTailSettlesAndStartsPromotedBatch(t *testing.T) {
	var calls []string
	tail := applicationconversation.NewTurnTail(applicationconversation.TurnTailDeps{
		RebindWorkspaceWhenIdle: func() { calls = append(calls, "rebind") },
		MarkPromotedConsumed: func(_ session_runtime.Location, sessionID, requestID string) error {
			calls = append(calls, "mark:"+sessionID+":"+requestID)
			return errors.New("mark failed")
		},
		Publish: func(applicationconversation.Publication) { calls = append(calls, "publish") },
		StartNext: func(_ context.Context, sessionID, requestID string, _ applicationconversation.Request) {
			calls = append(calls, "start:"+sessionID+":"+requestID)
		},
		ReconcileSessionRelease: func() { calls = append(calls, "release") },
		FlushToolRecord:         func(string) { calls = append(calls, "flush") },
	})

	tail.Complete(session_runtime.Location{}, applicationconversation.Publication{
		SessionID: "session-a",
		RequestID: "turn-a",
		Promotion: applicationconversation.Promotion{
			Promoted:  true,
			RequestID: "turn-b",
		},
	}, context.Background())

	want := []string{
		"rebind",
		"mark:session-a:turn-b",
		"publish",
		"start:session-a:turn-b",
		"release",
		"flush",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
