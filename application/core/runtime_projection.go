package core

import (
	"context"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type serviceRuntimeProjectionPort struct {
	service *Service
}

func (port serviceRuntimeProjectionPort) CollectProjection(ctx context.Context) view_state.RuntimeStateProjection {
	return port.service.components.view.CollectRuntimeProjection(ctx)
}

func (port serviceRuntimeProjectionPort) LockView() {
	port.service.ViewMu.Lock()
}

func (port serviceRuntimeProjectionPort) ApplyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	port.service.components.view.ApplyRuntimeProjectionLocked(projection)
}

func (port serviceRuntimeProjectionPort) BumpLocked() uint64 {
	return port.service.components.view.BumpLocked()
}

func (port serviceRuntimeProjectionPort) UnlockView() {
	port.service.ViewMu.Unlock()
}

func (port serviceRuntimeProjectionPort) CurrentViewSessionID() string {
	return port.service.currentViewSessionID()
}

func (port serviceRuntimeProjectionPort) RuntimeSnapshot() model.RuntimeState {
	return port.service.components.frontendSnapshot.Snapshot().Runtime
}

func (port serviceRuntimeProjectionPort) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState) {
	port.service.publishSessionEvent(EventRuntimeChanged, revision, "", sessionID, runtime)
}

func (port serviceRuntimeProjectionPort) PublishTaskDeltas() {
	port.service.publishTaskDeltas()
}

// publishRuntimeProjections copies application-owned state under the view lock
// and publishes immutable values after unlocking.
func (service *Service) publishRuntimeProjections() {
	service.ViewMu.RLock()
	var goalGovernance *dto.GoalGovernanceView
	if service.components.goal != nil {
		goalGovernance = service.components.goal.GoalGovernanceViewFor(service.Core.Snapshot.Session.ID)
	}
	projection := applicationadmin.RuntimeVisibility(service.components.tasks.GoalSkillActive(), goalGovernance)
	evidence := applicationadmin.ParentEvidence(
		service.Core.Snapshot.Session.ID,
		service.Core.Snapshot.Conversation,
		service.Core.Snapshot.TotalMessages,
	)
	service.ViewMu.RUnlock()
	service.Deps.Runtime.SetRuntimeVisibilityProjection(projection)
	service.Deps.Runtime.SetParentEvidenceProjection(evidence)
}
