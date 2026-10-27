package conversation_test

import (
	"context"
	"errors"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/session"
)

func TestConversationTurnFinalizeMapsCommitToPublication(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	promotion := applicationconversation.Promotion{Promoted: true, RequestID: "turn-b", Context: nil}
	nextUser := &model.Message{ID: "user"}
	nextAssistant := &model.Message{ID: "assistant"}
	turnErr := errors.New("turn failed")
	var gotLocation session_runtime.Location
	var gotPublication applicationconversation.Publication
	finalize := applicationconversation.NewTurnFinalize(applicationconversation.TurnFinalizeDeps{
		TailComplete: func(got session_runtime.Location, publication applicationconversation.Publication, nextContext context.Context) {
			gotLocation = got
			gotPublication = publication
		},
	})

	finalize.Finalize(applicationconversation.CommitResult{
		SessionID: "session-a", RequestID: "turn-a",
		Location: location, TurnErr: turnErr, VisibleError: "visible",
		Promotion: promotion, NextUser: nextUser, NextAssistant: nextAssistant, Revision: 9,
	})

	if gotLocation != location {
		t.Fatalf("location = %+v", gotLocation)
	}
	if gotPublication.SessionID != "session-a" || gotPublication.RequestID != "turn-a" ||
		gotPublication.Revision != 9 || gotPublication.VisibleError != "visible" ||
		gotPublication.TurnErr != turnErr || gotPublication.Promotion.RequestID != "turn-b" ||
		gotPublication.NextUser != nextUser || gotPublication.NextAssistant != nextAssistant {
		t.Fatalf("publication = %+v", gotPublication)
	}
}

func TestConversationTurnFinalizeIgnoresStaleCommit(t *testing.T) {
	finalize := applicationconversation.NewTurnFinalize(applicationconversation.TurnFinalizeDeps{
		TailComplete: func(session_runtime.Location, applicationconversation.Publication, context.Context) {
			t.Fatal("stale commit 不得发布或启动 tail")
		},
	})

	finalize.Finalize(applicationconversation.CommitResult{Stale: true})
}
