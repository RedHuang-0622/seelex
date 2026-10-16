package core

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	view "github.com/RedHuang-0622/seelex/middle/view"
)

// publishRuntimeProjections 在 service.ViewMu 下拷贝应用自有状态，释放锁后发布
// 不可变值。Runtime 因此不会从工具可见性或子代理路径回调 Application。
//
// 投影的构造归中间层读数面 `middle/view`（plan §2.4：`runtime_projection.go`
// → `middle/view/`）；这里只保留"锁下取数、锁外发布"这一段宿主责任。顺带
// 消掉了一条越层边：投影形状本来取自身为适配层的 `assemble`，现在直接用
// 契约层的 `dto`。
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
