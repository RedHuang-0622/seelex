package cold_test

import (
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

func TestSessionColdActivationPreparesActiveSession(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	unit.SetCancel(func() {})
	snapshot := model.Snapshot{Session: model.SessionState{ID: "session-a", Status: model.SessionStatusRestoring}}
	var calls []string
	activation := cold.NewActivation(cold.ActivationDeps{
		ClearRestoring: func(sessionID string) { calls = append(calls, "clear:"+sessionID) },
		ViewIntent:     func(string) uint64 { return 9 },
		TitleFromHistory: func([]contract.EngineMessage) string {
			return "legacy title"
		},
		SetActive: func(sessionID string) { calls = append(calls, "active:"+sessionID) },
		UnitFor:   func(string) *sessioncase.SessionUnit { return unit },
		ApplyStoredTier: func(sessionID, tier string) {
			calls = append(calls, "tier:"+tier)
		},
		SetTitle: func(sessionID string, title model.SessionTitle) {
			calls = append(calls, "title:"+title.Value+":"+title.Source)
		},
	})
	record := model.SessionRecord{Title: model.SessionTitle{Value: "record title", Source: "first_request"}}

	got := activation.PrepareLocked(
		"session-a", 9, &snapshot,
		[]contract.EngineMessage{{Role: "user", Content: "one"}},
		record, true, "auto",
	)

	if !got.MayActivate || got.Name != "record title" {
		t.Fatalf("activation = %+v", got)
	}
	if snapshot.Session.Name != "record title" || snapshot.Session.ID != "session-a" {
		t.Fatalf("snapshot session = %+v", snapshot.Session)
	}
	if unit.CancelFunc() != nil {
		t.Fatal("cold activation must clear cancel")
	}
	want := []string{"clear:session-a", "active:session-a", "tier:auto", "title:record title:legacy_history", "title:record title:first_request"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestSessionColdActivationKeepsViewWhenEpochOrTargetChanged(t *testing.T) {
	tests := []struct {
		name   string
		epoch  uint64
		viewID string
	}{
		{name: "old-epoch", epoch: 8, viewID: "session-a"},
		{name: "view-moved", epoch: 9, viewID: "session-b"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unit, err := sessioncase.NewSessionUnit("session-a")
			if err != nil {
				t.Fatal(err)
			}
			snapshot := model.Snapshot{Session: model.SessionState{ID: test.viewID, Name: "current"}}
			activation := cold.NewActivation(cold.ActivationDeps{
				ClearRestoring:   func(string) {},
				ViewIntent:       func(string) uint64 { return 9 },
				TitleFromHistory: func([]contract.EngineMessage) string { return "legacy" },
				SetActive:        func(string) { t.Fatal("superseded load must not activate") },
				UnitFor:          func(string) *sessioncase.SessionUnit { return unit },
				ApplyStoredTier:  func(string, string) {},
				SetTitle:         func(string, model.SessionTitle) {},
			})

			got := activation.PrepareLocked("session-a", test.epoch, &snapshot, nil, model.SessionRecord{}, false, "")

			if got.MayActivate {
				t.Fatal("superseded load must not activate")
			}
			if snapshot.Session.ID != test.viewID || snapshot.Session.Name != "current" {
				t.Fatalf("view snapshot changed: %+v", snapshot.Session)
			}
		})
	}
}
