package assembly

import (
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// Plan 执行引擎已退役；本文件只保留**历史 Plan 的只读投影**：旧会话里
// 已落盘的 plan 栈/投影仍可在切换会话时显示，但不再产生新的执行事件。

// planProjectionLocked 返回指定会话的历史 Plan 显示投影（调用方持有 Core.ViewMu）。
func (service *Service) planProjectionLocked(sessionID string) *model.PlanState {
	if sessionID == "" || sessionID == service.Core.Snapshot.Session.ID {
		return service.Core.Snapshot.Runtime.Plan
	}
	return service.components.tasks.PlanProjectionFor(sessionID, func() *model.PlanState {
		return task_context.ActivePlanFromStack(
			service.components.tasks.PlanStackFor(sessionID),
			service.components.tasks.ActivePlanIDFor(sessionID),
		)
	})
}

// mirrorPlanProjectionForSessionLocked 在会话成为视图会话时刷新
// Snapshot.Runtime.Plan 镜像（调用方持有 Core.ViewMu）：
//   - 若协调器投影已有（历史事件/其它路径推进过），取投影副本；
//   - 投影缺失时用 fallback（record/plan 栈）补种后再镜像。
func (service *Service) mirrorPlanProjectionForSessionLocked(sessionID string, fallback func() *model.PlanState) {
	plan := service.components.tasks.PlanProjectionCopy(sessionID)
	if plan == nil {
		plan = fallback()
		service.components.tasks.EnsurePlanProjection(sessionID, plan)
	}
	service.Core.Snapshot.Runtime.Plan = plan
}
