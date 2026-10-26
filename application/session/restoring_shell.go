package session

import (
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// RestoringShellDeps supplies title, chat, mirror and revision callbacks.
type RestoringShellDeps struct {
	Title            func(sessionID string) string
	SetActive        func(sessionID string)
	SetSessionChat   func(sessionID string, chat model.ChatState)
	MirrorActiveView func()
	BumpRevision     func() uint64
}

// RestoringShellApplication centralizes the loading-session snapshot shell.
type RestoringShellApplication struct {
	deps RestoringShellDeps
}

// NewRestoringShell constructs the restoring-shell application.
func NewRestoringShell(deps RestoringShellDeps) *RestoringShellApplication {
	return &RestoringShellApplication{deps: deps}
}

// PointLocked points the snapshot at a restoring session shell. The caller
// holds Core.ViewMu.
func (application *RestoringShellApplication) PointLocked(
	sessionID string,
	unit *sessioncase.SessionUnit,
	snapshot *model.Snapshot,
) uint64 {
	name := application.deps.Title(sessionID)
	snapshot.Session = model.SessionState{
		ID: sessionID, Name: name, Status: model.SessionStatusRestoring,
	}
	application.deps.SetActive(sessionID)
	snapshot.Task = nil
	snapshot.Runtime.Plan = nil
	snapshot.Runtime.TodoItems = nil
	snapshot.Runtime.SubAgentTree = nil
	snapshot.Runtime.WorkTable = nil
	snapshot.Runtime.WorkTableBatches = nil
	snapshot.Runtime.TeamworkBoard = nil
	snapshot.Interaction = nil
	application.deps.SetSessionChat(sessionID, unit.ChatState())
	application.deps.MirrorActiveView()
	return application.deps.BumpRevision()
}
