package conversation_test

import (
	"errors"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	viewerrors "github.com/RedHuang-0622/seelex/application/errorpresentation"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"strings"
	"testing"
)

func TestConversationTurnCompletionConfirmsPublishedTurn(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	var calls []string
	completion := applicationconversation.NewTurnCompletion(applicationconversation.TurnCompletionDeps{
		LocateSession: func(sessionID string) session_runtime.Location {
			calls = append(calls, "locate:"+sessionID)
			return location
		},
		PersistSession: func(got session_runtime.Location, sessionID string) error {
			calls = append(calls, "persist:"+sessionID)
			if got != location {
				t.Fatalf("persist location = %+v, want %+v", got, location)
			}
			return nil
		},
		QueueFailConsumedInput: func(session_runtime.Location, string, string) error {
			t.Fatal("成功发布后不得把输入退回草稿")
			return nil
		},
		QueueConfirmConsumed: func(got session_runtime.Location, sessionID, requestID string) error {
			calls = append(calls, "confirm:"+sessionID+":"+requestID)
			if got != location {
				t.Fatalf("confirm location = %+v, want %+v", got, location)
			}
			return nil
		},
		ReleaseWorkingHistory: func(sessionID string) {
			calls = append(calls, "release:"+sessionID)
		},
	})
	turnErr := errors.New("turn failed after persistence")

	gotLocation, gotErr := completion.Persist("session-a", "turn-a", turnErr)

	if gotLocation != location {
		t.Fatalf("location = %+v, want %+v", gotLocation, location)
	}
	if !errors.Is(gotErr, turnErr) {
		t.Fatalf("Persist error = %v, want original turn error", gotErr)
	}
	wantCalls := []string{"locate:session-a", "persist:session-a", "confirm:session-a:turn-a", "release:session-a"}
	if strings.Join(calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestConversationTurnCompletionReturnsConsumedInputOnPersistenceFailure(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	saveErr := errors.New("disk unavailable")
	turnErr := errors.New("provider failed")
	var failed bool
	var confirmed bool
	var released bool
	completion := applicationconversation.NewTurnCompletion(applicationconversation.TurnCompletionDeps{
		LocateSession: func(string) session_runtime.Location { return location },
		PersistSession: func(session_runtime.Location, string) error {
			return saveErr
		},
		QueueFailConsumedInput: func(got session_runtime.Location, sessionID, requestID string) error {
			failed = got == location && sessionID == "session-a" && requestID == "turn-a"
			return nil
		},
		QueueConfirmConsumed: func(session_runtime.Location, string, string) error {
			confirmed = true
			return nil
		},
		ReleaseWorkingHistory: func(string) {
			released = true
		},
	})

	_, gotErr := completion.Persist("session-a", "turn-a", turnErr)

	if !failed {
		t.Fatal("持久化失败时必须把消费项退回草稿")
	}
	if confirmed {
		t.Fatal("持久化失败时不得确认消费项")
	}
	if released {
		t.Fatal("持久化失败时不得释放工作历史")
	}
	if !errors.Is(gotErr, turnErr) || !strings.Contains(gotErr.Error(), saveErr.Error()) {
		t.Fatalf("Persist error = %v, want turn error identity and save error text", gotErr)
	}
	if code := viewerrors.StructuredCode(gotErr); code != contract.ErrorCodePersistenceFailed {
		t.Fatalf("structured code = %q, want %q", code, contract.ErrorCodePersistenceFailed)
	}
}
