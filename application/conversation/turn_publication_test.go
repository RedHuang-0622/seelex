package conversation_test

import (
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestConversationTurnPublisherPublishesSnapshotWithoutPromotion(t *testing.T) {
	var calls []string
	publisher := applicationconversation.NewTurnPublisher(applicationconversation.TurnPublisherDeps{
		PublishChatState: func(sessionID string) {
			calls = append(calls, "chat:"+sessionID)
		},
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, _ any) {
			calls = append(calls, string(kind)+":"+requestID+":"+sessionID)
		},
		SetCurrentTaskBatch: func(string, string) {
			t.Fatal("无队列提升时不得切换任务批次")
		},
		PublishRuntimeProjections: func() {
			t.Fatal("无队列提升时不得发布下一轮 runtime projection")
		},
	})

	publisher.Publish(applicationconversation.Publication{
		SessionID: "session-a",
		RequestID: "turn-a",
		Revision:  7,
	})

	want := []string{
		"chat:session-a",
		string(event.EventSnapshotChanged) + ":turn-a:session-a",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnPublisherReportsErrorBeforePromotedBatch(t *testing.T) {
	var calls []string
	publisher := applicationconversation.NewTurnPublisher(applicationconversation.TurnPublisherDeps{
		PublishChatState: func(sessionID string) {
			calls = append(calls, "chat:"+sessionID)
		},
		PublishEvent: func(kind event.EventKind, _ uint64, requestID, sessionID string, payload any) {
			call := string(kind) + ":" + requestID + ":" + sessionID
			switch value := payload.(type) {
			case model.Message:
				call += ":" + value.ID
			case map[string]string:
				call += ":" + value["message"]
			}
			calls = append(calls, call)
		},
		SetCurrentTaskBatch: func(sessionID, requestID string) {
			calls = append(calls, "batch:"+sessionID+":"+requestID)
		},
		PublishRuntimeProjections: func() {
			calls = append(calls, "projections")
		},
	})

	publisher.Publish(applicationconversation.Publication{
		SessionID:    "session-a",
		RequestID:    "turn-a",
		Revision:     8,
		VisibleError: "provider down",
		TurnErr:      errors.New("provider down"),
		Promotion: applicationconversation.Promotion{
			Promoted:  true,
			RequestID: "turn-b",
		},
		NextUser:      &model.Message{ID: "user-b"},
		NextAssistant: &model.Message{ID: "assistant-b"},
	})

	want := []string{
		"chat:session-a",
		string(event.EventError) + ":turn-a:session-a:provider down",
		string(event.EventMessageAdded) + ":turn-b:session-a:user-b",
		string(event.EventMessageAdded) + ":turn-b:session-a:assistant-b",
		"batch:session-a:turn-b",
		"projections",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
