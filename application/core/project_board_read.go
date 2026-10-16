package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/view/board"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
	"github.com/RedHuang-0622/seelex/store"
)

// ---------------------------------------------------------------------------
// 4) Service 侧的只读取数口（M3 装配用；本项**不接钩子**）
// ---------------------------------------------------------------------------

// WorkTableRowsFor 返回指定会话的**活体工作表格投影**（只读；不重算任何状态）。
//
// 取数路径与实时重投影**同一份实现**（buildWorkTable + 按会话取 task 快照 + 既有
// 子代理树/后台执行投影）：投影行因此天然等于"此刻工作表格里那几行"，投影侧不新增
// 状态判定（I3）。后台执行（async）行也在此列——它就是既有活体投影的一部分（工作
// 表格是跨会话台账），行自带 session_id 供读侧归属。
func (service *Service) WorkTableRowsFor(sessionID string) []WorkItem {
	if service == nil || service.Deps.Runtime == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	service.ViewMu.RLock()
	viewSessionID := service.Core.Snapshot.Session.ID
	plan := service.sessionActivePlanLocked(sessionID)
	tree := worktable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	service.ViewMu.RUnlock()
	return worktable.BuildWorkTable(
		plan,
		service.taskRecordsForSession(sessionID, viewSessionID),
		tree,
		service.asyncRunsForTable(),
		workTableBounds(),
	)
}

// ProjectRootFor 返回会话所属工作区的**项目根**（绝对路径；"" = 未绑定/无落点）。
//
// 项目根的权威读法是 WorkspacePort.SessionWorkspace（与 bindSessionProjectRoot /
// restoreInitialWorkspace 同源）——看板的落点就是 `<项目根>/.seelex/board/`，
// 所以"项目根"这一格必须来自**绑定事实**，不能由视图/活跃工作区猜。
//
// 未绑定工作区的会话返回 ""（**没有落点**：不落盘，契约 §3）。这里**不**回退到
// Sessions.Workspace()：那是"视图当前工作区"，拿它当落点等于把别的项目的看板写进去。
func (service *Service) ProjectRootFor(sessionID string) string {
	if service == nil || service.Deps.Workspace == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID); ok {
		return strings.TrimSpace(workspace.RootPath)
	}
	return ""
}

// ProjectBoardTeamPlanFor 按**窄可选端口**取 teamwork 计划（未装配 / 该会话没有计划 ⇒ nil）。
//
// 这一步（端口探测 + 收窄）只写在这里：M3 装配时把它作为 ProjectBoardDeps.TeamPlanFor
// 注入即可，同步器不必知道 Runtime 的接口形状。
func (service *Service) ProjectBoardTeamPlanFor(sessionID string) *store.TeamworkPlan {
	if service == nil || service.Deps.Runtime == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	source, ok := service.Deps.Runtime.(board.TeamworkPlanSource)
	if !ok {
		return nil
	}
	plan, ok := source.TeamworkPlanFor(sessionID)
	if !ok {
		return nil
	}
	return &plan
}

// ProjectBoardTeamStateFor 取该会话的 teamwork **运行态**（里程碑结论 / 整队收口）。
//
// 运行态就在计划载荷的那一格里（TeamworkPlan.State 是 TeamworkState，见
// store/teamwork.go:98）：读它而不是另找一份，是"整队收口只有一个权威格"这条
// 裁决的落地。
func (service *Service) ProjectBoardTeamStateFor(sessionID string) *store.TeamworkState {
	plan := service.ProjectBoardTeamPlanFor(sessionID)
	if plan == nil {
		return nil
	}
	state := plan.State
	return &state
}

// taskRecordsForSession 按会话取 task 注册表快照：视图/活跃会话 = 实时注册表（"" 分区），
// 后台会话 = 自身 scope 分区；Runtime 未实现 TaskSnapshotFor 时退化为全局快照。
//
// **唯一实现**：打点表（workTableTraceBlockFor）与看板取数（WorkTableRowsFor）走同一份，
// 避免"同一条会话作用域判据写两遍"（两遍迟早不一致：一份改了、另一份没改）。
func (state *serviceState) taskRecordsForSession(sessionID, viewSessionID string) []dto.TaskRecord {
	if state == nil || state.Deps.Runtime == nil {
		return nil
	}
	forTasks, ok := state.Deps.Runtime.(interface{ TaskSnapshotFor(string) []dto.TaskRecord })
	if !ok {
		return state.Deps.Runtime.TaskSnapshot()
	}
	scope := sessionID
	if scope == "" || scope == viewSessionID {
		// 视图/活跃会话的 task scope 恒为实时注册表（"" 分区）。
		scope = ""
	}
	return forTasks.TaskSnapshotFor(scope)
}
