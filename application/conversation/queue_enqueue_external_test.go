package conversation_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestQueueEnqueueProjectsAndPublishesRevision(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	var mirroredChat string
	queue := applicationconversation.NewQueue(applicationconversation.QueueDeps{
		TurnState: applicationconversation.NewTurnState(),
		MirrorChatState: func(sessionID string, chat model.ChatState) {
			if sessionID != "session-a" {
				t.Fatalf("mirror session = %q", sessionID)
			}
			mirroredChat = chat.InputQueue[0]
		},
		BumpRevision: func() uint64 { return 9 },
	})
	request := applicationconversation.Request{DisplayInput: "queued"}

	revision := queue.EnqueueLocked("session-a", unit, request, true)

	if revision != 9 {
		t.Fatalf("revision = %d, want 9", revision)
	}
	if mirroredChat != "queued" || unit.ChatState().QueuedCount != 1 {
		t.Fatalf("queued chat = %q state=%+v", mirroredChat, unit.ChatState())
	}
}

func TestQueueAfterEnqueuePublishesThenMirrorsDurably(t *testing.T) {
	var calls []string
	durableErr := errors.New("write failed")
	queue := applicationconversation.NewQueue(applicationconversation.QueueDeps{
		PublishSnapshot: func(revision uint64, sessionID string) {
			calls = append(calls, "snapshot")
		},
		PublishChatState: func(sessionID string) {
			calls = append(calls, "chat")
		},
		DurableEnqueue: func(sessionID, input string) error {
			calls = append(calls, "durable")
			return durableErr
		},
		Debug: func(format string, args ...any) {
			calls = append(calls, "debug")
		},
	})

	queue.AfterEnqueue("session-a", 3, applicationconversation.Request{DisplayInput: "queued"})

	want := []string{"snapshot", "chat", "durable", "debug"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestQueueDrainLockedCombinesPendingBeforeCurrentRequest(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	pending := applicationconversation.Request{DisplayInput: "pending"}
	unit.Enqueue(sessioncase.QueuedRequest{DisplayInput: pending.DisplayInput, Payload: pending})
	queue := applicationconversation.NewQueue(applicationconversation.QueueDeps{
		TurnState: applicationconversation.NewTurnState(),
		Requests: applicationconversation.NewRequestApplication(applicationconversation.RequestDeps{
			ParseModelEnvelope: func(string) (string, int, bool) { return "", 0, false },
			WrapModelInput:     func(display, _ string) string { return display },
		}),
	})

	merged, drained := queue.DrainLocked(unit, applicationconversation.Request{DisplayInput: "current"})

	if !drained {
		t.Fatal("pending queue should be drained")
	}
	if merged.DisplayInput != "pending\n---\ncurrent" {
		t.Fatalf("merged input = %q", merged.DisplayInput)
	}
	if len(unit.PendingRequests()) != 0 {
		t.Fatalf("pending requests remain: %+v", unit.PendingRequests())
	}
}

func TestQueueRecoverInputsProjectsOnlyNonEmptyRows(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.RWMutex
	var calls []string
	queue := applicationconversation.NewQueue(applicationconversation.QueueDeps{
		ViewMu:    &mu,
		TurnState: applicationconversation.NewTurnState(),
		IsActive:  func(string) bool { return true },
		MirrorChatState: func(_ string, chat model.ChatState) {
			calls = append(calls, "mirror")
		},
		BumpRevision: func() uint64 {
			calls = append(calls, "revision")
			return 4
		},
		PublishSnapshot: func(revision uint64, sessionID string) {
			calls = append(calls, "snapshot")
		},
		PublishChatState: func(sessionID string) {
			calls = append(calls, "chat")
		},
		NewRequest: func(input string, layers []prompt.PromptLayer) applicationconversation.Request {
			return applicationconversation.Request{DisplayInput: strings.TrimSpace(input), Skills: layers}
		},
		SessionUnit: func(string) applicationconversation.QueueUnit { return unit },
	})

	queue.RecoverInputs("session-a", []dto.QueueItem{
		{Content: "  recovered  "},
		{Content: "   "},
	}, []prompt.PromptLayer{{Kind: "skill", Name: "review"}})

	if len(unit.PendingRequests()) != 1 || unit.ChatState().QueuedCount != 1 {
		t.Fatalf("recovered queue = %+v", unit.PendingRequests())
	}
	if got := strings.Join(calls, ","); got != "mirror,revision,snapshot,chat" {
		t.Fatalf("calls = %q", got)
	}
}
