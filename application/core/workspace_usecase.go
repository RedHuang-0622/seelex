package core

import (
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// releaseSessionAsync tells the execution domain that a session's background
// commands must be terminated.
func (service *Service) releaseSessionAsync(sessionID string) {
	if service == nil || service.Deps.Runtime == nil || sessionID == "" {
		return
	}
	service.Deps.Runtime.ReleaseSessionAsync(sessionID)
}

// applyWorkspaceProjectionLocked copies a middle-layer projection into the
// authoritative application snapshot. The caller holds Core.ViewMu.
func (service *Service) applyWorkspaceProjectionLocked(projection switchworkspace.Projection) {
	service.Core.Snapshot.Workspaces = append([]model.WorkspaceInfo(nil), projection.Workspaces...)
	service.Core.Snapshot.SessionWorkspaces = make(map[string]string, len(projection.Bindings))
	for sessionID, workspaceID := range projection.Bindings {
		service.Core.Snapshot.SessionWorkspaces[sessionID] = workspaceID
	}
}

type serviceWorkspaceProjectionPort struct {
	service *Service
}

func (port serviceWorkspaceProjectionPort) Projection() switchworkspace.Projection {
	return port.service.components.workspace.Projection()
}

func (port serviceWorkspaceProjectionPort) LockView() {
	port.service.ViewMu.Lock()
}

func (port serviceWorkspaceProjectionPort) ApplyWorkspaceProjectionLocked(projection switchworkspace.Projection) {
	port.service.applyWorkspaceProjectionLocked(projection)
}

func (port serviceWorkspaceProjectionPort) BumpLocked() uint64 {
	return port.service.components.view.BumpLocked()
}

func (port serviceWorkspaceProjectionPort) UnlockView() {
	port.service.ViewMu.Unlock()
}

func (port serviceWorkspaceProjectionPort) CurrentViewSessionID() string {
	return port.service.currentViewSessionID()
}

func (port serviceWorkspaceProjectionPort) PublishSnapshotChanged(revision uint64, sessionID string) {
	port.service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
}
