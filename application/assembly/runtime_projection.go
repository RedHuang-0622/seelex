package assembly

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
	port.service.components.workTableRuntime.PublishTaskDeltas()
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

// PublishInitialRuntimeProjections 是组合根触发**首次**运行时投影发布的唯一入口。
//
// 它刻意不进组件快照、也不挂到任何前端方法面：这件事要读 ViewMu 下的核心快照再写
// Runtime 投影，是 assembly 自己的内部协调，只在"装配完成、Deps.Runtime 已就位"
// 之后按一次。作为字段暴露出去，会让调用方以为它能像刷新读数那样随时重放。
func PublishInitialRuntimeProjections(service *Service) {
	service.publishRuntimeProjections()
}
