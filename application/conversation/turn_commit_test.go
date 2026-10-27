package conversation_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type recordingLocker struct{ locked int }

func (locker *recordingLocker) Lock()   { locker.locked++ }
func (locker *recordingLocker) Unlock() { locker.locked-- }

func TestConversationTurnCommitRunsPersistenceOutcomePromotion(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	locker := &recordingLocker{}
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	turnErr := errors.New("turn failed")
	promotion := applicationconversation.Promotion{
		Promoted:  true,
		RequestID: "turn-b",
		Request:   applicationconversation.Request{DisplayInput: "next"},
		UserEvent: model.TranscriptEvent{RoleName: "user", RoleSessionID: "session-a", RoundID: 2},
	}
	nextUser := &model.Message{ID: "user-b"}
	nextAssistant := &model.Message{ID: "assistant-b"}
	var calls []string
	commit := applicationconversation.NewTurnCommit(applicationconversation.TurnCommitDeps{
		Persist: func(sessionID, requestID string, gotErr error) (session_runtime.Location, error) {
			calls = append(calls, "persist")
			if gotErr != turnErr {
				t.Fatalf("persist err = %v", gotErr)
			}
			return location, turnErr
		},
		Lock:     func() sync.Locker { return locker },
		Unit:     func(sessionID string) *sessioncase.SessionUnit { return unit },
		IsActive: func(sessionID string) bool { return true },
		ApplyOutcome: func(sessionID, requestID string, gotUnit *sessioncase.SessionUnit, projection viewstate.RuntimeStateProjection, gotErr error) (string, bool) {
			calls = append(calls, "outcome")
			return "visible", false
		},
		Promote: func(sessionID string, gotUnit *sessioncase.SessionUnit) applicationconversation.Promotion {
			calls = append(calls, "promote")
			return promotion
		},
		AppendNext: func(sessionID string, active bool, got applicationconversation.Promotion) (*model.Message, *model.Message) {
			calls = append(calls, "append")
			if !active || got.RequestID != promotion.RequestID {
				t.Fatalf("append args = %v/%+v", active, got)
			}
			return nextUser, nextAssistant
		},
		Mirror: func(sessionID string, gotUnit *sessioncase.SessionUnit, active bool) uint64 {
			calls = append(calls, "mirror")
			return 9
		},
		Stale: func(string, string, *sessioncase.SessionUnit) {
			t.Fatal("stale callback must not run")
		},
	})

	got := commit.Commit("session-a", "turn-a", turnErr, viewstate.RuntimeStateProjection{})

	if got.Location != location || got.TurnErr != turnErr || got.VisibleError != "visible" ||
		got.Revision != 9 || got.Promotion.RequestID != "turn-b" ||
		got.NextUser != nextUser || got.NextAssistant != nextAssistant || got.Stale {
		t.Fatalf("commit result = %+v", got)
	}
	want := "persist,outcome,promote,append,mirror"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
	if locker.locked != 0 {
		t.Fatalf("lock leaked: %d", locker.locked)
	}
}

func TestConversationTurnCommitStopsOnStaleRequest(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	locker := &recordingLocker{}
	var staleCalled bool
	commit := applicationconversation.NewTurnCommit(applicationconversation.TurnCommitDeps{
		Persist: func(string, string, error) (session_runtime.Location, error) {
			return session_runtime.Location{}, nil
		},
		Lock:     func() sync.Locker { return locker },
		Unit:     func(string) *sessioncase.SessionUnit { return unit },
		IsActive: func(string) bool { return false },
		ApplyOutcome: func(string, string, *sessioncase.SessionUnit, viewstate.RuntimeStateProjection, error) (string, bool) {
			return "", true
		},
		Promote: func(string, *sessioncase.SessionUnit) applicationconversation.Promotion {
			t.Fatal("stale request must not promote")
			return applicationconversation.Promotion{}
		},
		AppendNext: func(string, bool, applicationconversation.Promotion) (*model.Message, *model.Message) {
			t.Fatal("stale request must not append")
			return nil, nil
		},
		Mirror: func(string, *sessioncase.SessionUnit, bool) uint64 {
			t.Fatal("stale request must not mirror")
			return 0
		},
		Stale: func(string, string, *sessioncase.SessionUnit) { staleCalled = true },
	})

	got := commit.Commit("session-a", "turn-a", nil, viewstate.RuntimeStateProjection{})

	if !got.Stale || !staleCalled || locker.locked != 0 {
		t.Fatalf("commit = %+v staleCalled=%v locks=%d", got, staleCalled, locker.locked)
	}
}
