package adapters

import (
	"errors"
	"reflect"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	middleresidency "github.com/RedHuang-0622/seelex/middle/residency"
)

func TestResidencyPortMatchesMiddlePureDecisions(t *testing.T) {
	port := NewResidencyPort()

	preview, truncated := port.Preview("abcdef", 3)
	wantPreview, wantTruncated := middleresidency.Preview("abcdef", 3)
	if preview != wantPreview || truncated != wantTruncated {
		t.Fatalf("Preview = (%q,%v), want (%q,%v)", preview, truncated, wantPreview, wantTruncated)
	}

	order := []string{"a", "b", "c"}
	if got, want := port.Touch(order, "b"), []string(middleresidency.Order(order).Touch("b")); !reflect.DeepEqual(got, want) {
		t.Fatalf("Touch = %v, want %v", got, want)
	}
	forgetPort := append([]string(nil), order...)
	forgetMiddle := append([]string(nil), order...)
	if got, want := port.Forget(forgetPort, "b"), []string(middleresidency.Order(forgetMiddle).Forget("b")); !reflect.DeepEqual(got, want) {
		t.Fatalf("Forget = %v, want %v", got, want)
	}
	if got, want := port.Budget(6, 2, 1), middleresidency.Budget(6, 2, 1); got != want {
		t.Fatalf("Budget = %d, want %d", got, want)
	}

	conversation := []model.Message{
		{ID: "u1", Role: "user", Content: "first"},
		{ID: "a1", Role: "assistant", Content: "reply"},
		{ID: "u2", Role: "user", Content: "second"},
	}
	userText := func(message model.Message) string {
		if message.Role == "user" {
			return message.Content
		}
		return ""
	}
	got := port.BuildInputIndex(conversation, []string{"second"}, 20, userText)
	want := middleresidency.BuildInputIndex(conversation, []string{"second"}, 20, userText)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildInputIndex mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestResidencyPortPreservesArchiveErrorIdentity(t *testing.T) {
	if !errors.Is(applicationsession.ErrArchiveNotFound, contract.ErrSessionNotFoundArchive) ||
		!errors.Is(middleresidency.ErrArchiveNotFound, contract.ErrSessionNotFoundArchive) {
		t.Fatal("archive not-found error identity diverged")
	}
}

func TestResidencyPortEvictionMatchesMiddleDecision(t *testing.T) {
	port := NewResidencyPort()
	var portEvicted, middleEvicted []string
	newSnapshot := func() applicationsession.EvictionSnapshot {
		return applicationsession.EvictionSnapshot{
			Order:       []string{"a", "b", "c"},
			Occupied:    map[string]bool{"a": true, "b": true, "c": true},
			Protected:   map[string]bool{"a": true},
			LegacyLimit: 0,
			Window:      1,
		}
	}
	portSnapshot := newSnapshot()
	port.Evict(applicationsession.EvictionDeps{
		Snapshot: func() applicationsession.EvictionSnapshot { return portSnapshot },
		Evict: func(sessionID string) error {
			portEvicted = append(portEvicted, sessionID)
			delete(portSnapshot.Occupied, sessionID)
			return nil
		},
	})
	middleSnapshot := newSnapshot()
	middleresidency.EvictUntilWithinLimit(middleresidency.EvictionDeps{
		Snapshot: func() middleresidency.EvictionSnapshot {
			return middleresidency.EvictionSnapshot{
				Order:       middleresidency.Order(middleSnapshot.Order),
				Occupied:    middleSnapshot.Occupied,
				Protected:   middleSnapshot.Protected,
				LegacyLimit: middleSnapshot.LegacyLimit,
				Window:      middleSnapshot.Window,
			}
		},
		Evict: func(sessionID string) error {
			middleEvicted = append(middleEvicted, sessionID)
			delete(middleSnapshot.Occupied, sessionID)
			return nil
		},
	})
	if !reflect.DeepEqual(portEvicted, middleEvicted) {
		t.Fatalf("eviction order = %v, want %v", portEvicted, middleEvicted)
	}
}
