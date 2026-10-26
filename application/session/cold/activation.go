package cold

import (
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ActivationResult is the prepared active-session projection.
type ActivationResult struct {
	MayActivate bool
	Name        string
	Unit        *sessioncase.SessionUnit
}

// ActivationDeps supplies locked activation callbacks.
type ActivationDeps struct {
	ClearRestoring   func(sessionID string)
	ViewIntent       func(sessionID string) uint64
	TitleFromHistory func(history []contract.EngineMessage) string
	SetActive        func(sessionID string)
	UnitFor          func(sessionID string) *sessioncase.SessionUnit
	ApplyStoredTier  func(sessionID, tier string)
	SetTitle         func(sessionID string, title model.SessionTitle)
}

// ActivationApplication prepares a loaded session under the view lock.
type ActivationApplication struct {
	deps ActivationDeps
}

// NewActivation constructs the cold activation application.
func NewActivation(deps ActivationDeps) *ActivationApplication {
	return &ActivationApplication{deps: deps}
}

// PrepareLocked applies epoch, active, tier and title decisions.
func (application *ActivationApplication) PrepareLocked(
	sessionID string,
	epoch uint64,
	snapshot *model.Snapshot,
	history []contract.EngineMessage,
	record model.SessionRecord,
	hasRecord bool,
	storedTier string,
) ActivationResult {
	mayActivate := application.deps.ViewIntent(sessionID) == epoch && snapshot.Session.ID == sessionID
	application.deps.ClearRestoring(sessionID)
	name := application.deps.TitleFromHistory(history)
	if hasRecord && record.Title.Value != "" {
		name = record.Title.Value
	}
	if mayActivate {
		snapshot.Session = model.SessionState{ID: sessionID, Name: name}
		application.deps.SetActive(sessionID)
	}
	unit := application.deps.UnitFor(sessionID)
	unit.SetCancel(nil)
	application.deps.ApplyStoredTier(sessionID, storedTier)
	application.deps.SetTitle(sessionID, model.SessionTitle{Value: name, Source: "legacy_history"})
	if hasRecord {
		application.deps.SetTitle(sessionID, record.Title)
	}
	return ActivationResult{MayActivate: mayActivate, Name: name, Unit: unit}
}
