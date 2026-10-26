package view_state

import (
	"testing"
	"time"

	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
)

func TestCoordinatorSubscribeSessionRoutesByViewOwnership(t *testing.T) {
	hub := event.NewEventHub()
	units := session.NewDomain()
	defer units.Close()
	units.SetActive("active")
	coordinator := NewCoordinator(Deps{
		Core:  state.New(contract.Dependencies{Events: hub}),
		Units: units,
	})

	subscription, err := coordinator.SubscribeSession("", 8)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	hub.PublishSession(event.EventMessageAdded, 1, "req-active", "active", "active")
	hub.PublishSession(event.EventMessageAdded, 2, "req-other", "other", "other")
	hub.PublishSession(event.EventResyncRequired, 3, "req-global", "", "global")

	eventKinds := make([]event.EventKind, 0, 2)
	for len(eventKinds) < 2 {
		select {
		case delivered := <-subscription.Events:
			eventKinds = append(eventKinds, delivered.Kind)
		case <-time.After(time.Second):
			t.Fatalf("session subscription delivered %v, want active and global events", eventKinds)
		}
	}
	if eventKinds[0] != event.EventMessageAdded || eventKinds[1] != event.EventResyncRequired {
		t.Fatalf("session subscription order = %v, want [message.added resync.required]", eventKinds)
	}
	select {
	case unexpected := <-subscription.Events:
		t.Fatalf("session subscription leaked other-session event: %+v", unexpected)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestCoordinatorSubscribeSessionWithReplayUsesExplicitScope(t *testing.T) {
	hub := event.NewEventHub()
	units := session.NewDomain()
	defer units.Close()
	coordinator := NewCoordinator(Deps{
		Core:  state.New(contract.Dependencies{Events: hub}),
		Units: units,
	})

	subscription, err := coordinator.SubscribeSessionWithReplay("explicit", 8, 4)
	if err != nil {
		t.Fatalf("SubscribeSessionWithReplay: %v", err)
	}
	defer subscription.Close()

	hub.PublishSession(event.EventMessageAdded, 1, "req-explicit", "explicit", "explicit")
	hub.PublishSession(event.EventMessageAdded, 2, "req-other", "other", "other")

	select {
	case delivered := <-subscription.Events:
		if delivered.SessionID != "explicit" {
			t.Fatalf("first replayed event session = %q, want explicit", delivered.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit session subscription delivered no event")
	}
	select {
	case unexpected := <-subscription.Events:
		t.Fatalf("explicit session subscription leaked other-session event: %+v", unexpected)
	case <-time.After(20 * time.Millisecond):
	}
	if replay := subscription.ReplaySince(0); !replay.Covered || len(replay.Events) != 1 {
		t.Fatalf("replay = %+v, want one covered explicit-session event", replay)
	}
}
