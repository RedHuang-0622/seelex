package core

// goal_service.go keeps the core-side runtime refresh and view-adapter seams.
// Goal tool parsing and routing live in application/goal.

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// teamJoinSeqFor 已随 `@` 手动召唤团队退场（2026-10-12 J-1a）删除：它的唯一调用方
// 是 application/core/input_team.go 的召唤落点（`MaterializeAgentTeam` 的 join 切点
// = 主会话当前已提交的 message 尾 seq）。会话团队装配面退场后没有装配来路，也就
// 没有"入伙切点"这个输入；角色会话号仍由 `RoleSnapshot` 那条既有读面提供（B1 保留
// 面，未动）。

// refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
// 槽（GoalGovernanceView 进 Snapshot.Runtime），使治理面板在直接工具/headless
// 调用路径也立即可见（不依赖下一次 ChatStream 的回合尾投影）。
func (service *Service) refreshGoalRuntimeProjection(sessionID string) {
	if service == nil || service.components.view == nil {
		return
	}
	projection := service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID)
	service.ViewMu.Lock()
	service.components.view.ApplyRuntimeProjectionForLocked(sessionID, projection)
	service.ViewMu.Unlock()
}

type goalGovernanceViewPort struct {
	service *Service
}

func (port goalGovernanceViewPort) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	if port.service == nil {
		return nil
	}
	return port.service.goalApplicationComponent().GovernanceViewFor(sessionID)
}
