package session_test

import (
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/event"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type interactionObserverLock struct {
	mu sync.Mutex
}

func (lock *interactionObserverLock) Lock()   { lock.mu.Lock() }
func (lock *interactionObserverLock) Unlock() { lock.mu.Unlock() }

type interactionObserverUnit struct {
	added   []string
	removed []string
}

func (unit *interactionObserverUnit) AddApproval(id string) { unit.added = append(unit.added, id) }
func (unit *interactionObserverUnit) RemoveApproval(id string) {
	unit.removed = append(unit.removed, id)
}

func TestInteractionObserverKeepsBackgroundApprovalOutOfViewSlot(t *testing.T) {
	lock := &interactionObserverLock{}
	view := &model.Snapshot{Session: model.SessionState{ID: "session-a"}}
	units := map[string]*interactionObserverUnit{
		"session-a": {},
		"session-b": {},
	}
	pending := map[string][]model.Interaction{}
	var calls []string
	application := applicationsession.NewInteractionObserver(applicationsession.InteractionObserverDeps{
		Lock:          lock,
		Snapshot:      view,
		ViewSessionID: func() string { return view.Session.ID },
		Unit: func(sessionID string) applicationsession.ApprovalUnit {
			return units[sessionID]
		},
		PendingBySession: func(sessionID string) []model.Interaction {
			return append([]model.Interaction(nil), pending[sessionID]...)
		},
		BumpRevision: func() uint64 { return 7 },
		PublishEvent: func(kind event.EventKind, _ uint64, _ string, sessionID string, _ any) {
			calls = append(calls, string(kind)+":"+sessionID)
		},
		PublishSessionStateChanged: func(sessionID string) {
			calls = append(calls, "state:"+sessionID)
		},
	})

	application.Observe("session-b", "ask-b", &model.Interaction{
		ID: "ask-b", Options: []model.InteractionOption{{ID: "allow"}},
	})

	if view.Interaction != nil {
		t.Fatalf("background approval entered view slot: %+v", view.Interaction)
	}
	if len(units["session-b"].added) != 1 {
		t.Fatalf("background unit approvals = %v", units["session-b"].added)
	}
	if got := joinObserverCalls(calls); got != "interaction.opened:session-b,state:session-b" {
		t.Fatalf("calls = %s", got)
	}

	pending["session-a"] = []model.Interaction{{ID: "ask-a", SessionID: "session-a"}}
	application.MirrorLocked("session-a")
	if view.Interaction == nil || view.Interaction.ID != "ask-a" {
		t.Fatalf("view approval mirror = %+v", view.Interaction)
	}
}

func joinObserverCalls(calls []string) string {
	out := ""
	for index, call := range calls {
		if index > 0 {
			out += ","
		}
		out += call
	}
	return out
}
