package core

import (
	"context"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	view "github.com/RedHuang-0622/seelex/middle/view"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

func (service *Service) runtimeApplicationComponent() *applicationadmin.RuntimeApplication {
	if service == nil {
		return nil
	}
	return service.components.runtimeApplication
}

func newServiceRuntimeApplication(service *Service) *applicationadmin.RuntimeApplication {
	return applicationadmin.NewRuntime(applicationadmin.RuntimeDeps{
		Projection: serviceRuntimeProjectionPort{service: service},
	})
}

type serviceRuntimeProjectionPort struct {
	service *Service
}

func (port serviceRuntimeProjectionPort) CollectProjection(ctx context.Context) view_state.RuntimeStateProjection {
	return port.service.collectRuntimeProjection(ctx)
}

func (port serviceRuntimeProjectionPort) LockView() {
	port.service.ViewMu.Lock()
}

func (port serviceRuntimeProjectionPort) ApplyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	port.service.applyRuntimeProjectionLocked(projection)
}

func (port serviceRuntimeProjectionPort) BumpLocked() uint64 {
	return port.service.bumpLocked()
}

func (port serviceRuntimeProjectionPort) UnlockView() {
	port.service.ViewMu.Unlock()
}

func (port serviceRuntimeProjectionPort) CurrentViewSessionID() string {
	return port.service.currentViewSessionID()
}

func (port serviceRuntimeProjectionPort) RuntimeSnapshot() model.RuntimeState {
	return port.service.Snapshot().Runtime
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
	projection := view.RuntimeVisibility(service.components.tasks.GoalSkillActive(), goalGovernance)
	evidence := view.ParentEvidence(
		service.Core.Snapshot.Session.ID,
		service.Core.Snapshot.Conversation,
		service.Core.Snapshot.TotalMessages,
	)
	service.ViewMu.RUnlock()
	service.Deps.Runtime.SetRuntimeVisibilityProjection(projection)
	service.Deps.Runtime.SetParentEvidenceProjection(evidence)
}
