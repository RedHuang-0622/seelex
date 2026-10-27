package assembly

// teamwork_service.go — Service 侧的**团队看板读面门面**。
//
// 读面本身（转发 + 未装配 / 不支持的宿主下的空视图口径）已于 2026-10-16 随
// M5-R8c 按 plan §2.7 归位 `backend/multiagent/teamwork`（`BoardReader`）；
// 这里只留 Service 方法这一层门面，前端与 TUI 的调用点零改动。
//
// 口径（契约 docs/arch/team-board-gui-tui-contract.md §3）不变：
//   - 事实在装配侧（计划与审计落 store 的 moduleTeamwork，作业行活在 jobs.Manager
//     的内存表里）。读面只做**转发**，不解释、不缓存、不做判定——缓存与失效的责任
//     在桥侧（见 backend/engine/assemble/runtime_teamwork_board.go）。
//   - 端口是**窄可选**的（contract.TeamworkBoardProjection）：未装配 teamwork 的
//     宿主（测试桩 / 精简宿主）走类型断言失败分支返回空视图，而不是让 RuntimePort
//     长出一个恒返回零值的成员。

import "github.com/RedHuang-0622/seelex/contract/dto"

// teamworkBoardViewFor 返回指定会话的团队看板只读投影（无计划 / 未装配 → nil，
// 前端与 TUI 据此整块退场，不留空壳）。
// teamworkBoardViewPort satisfies the view coordinator without a public Service method.
type teamworkBoardViewPort struct{ service *Service }

func (port teamworkBoardViewPort) TeamworkBoardViewFor(sessionID string) *dto.TeamworkBoardView {
	return port.service.components.executionApplication.BoardView(sessionID)
}
