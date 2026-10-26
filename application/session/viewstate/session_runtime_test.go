package view_state

import (
	"testing"

	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

type sessionRuntimeTasks struct{}

func (sessionRuntimeTasks) ActiveSkillIDs() []string   { return nil }
func (sessionRuntimeTasks) GoalSkillActive() bool      { return false }
func (sessionRuntimeTasks) RoleRoundFor(string) uint64 { return 0 }

func TestSessionViewRuntimeOwnsRestoringAndViewIntent(t *testing.T) {
	kernel := state.New(contract.Dependencies{})
	units := session.NewDomain()
	unit, err := session.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	units.Register(unit)
	coordinator := NewCoordinator(Deps{
		Core:   kernel,
		Units:  units,
		Tasks:  sessionRuntimeTasks{},
		Limits: func() dto.ViewLimits { return dto.ViewLimits{} },
		DraftView: func() (string, string) {
			return "", ""
		},
		TitleForSession: func(string) string { return "" },
	})

	coordinator.ViewMu.Lock()
	first := coordinator.MarkSessionViewIntentLocked("session-a")
	second := coordinator.MarkSessionViewIntentLocked("session-b")
	coordinator.SetRestoringLocked("session-a")
	if got := coordinator.SessionStatusLocked("session-a"); got != protocol.SessionStatusRestoring {
		t.Fatalf("status = %q, want restoring", got)
	}
	coordinator.ClearRestoringLocked("session-a")
	if first >= second || coordinator.SessionViewIntentLocked("session-a") != first {
		t.Fatalf("view intents first=%d second=%d current=%d", first, second, coordinator.SessionViewIntentLocked("session-a"))
	}
	if got := coordinator.SessionStatusLocked("session-a"); got != protocol.SessionStatusIdle {
		t.Fatalf("status after clear = %q, want idle", got)
	}
	coordinator.ViewMu.Unlock()
}
