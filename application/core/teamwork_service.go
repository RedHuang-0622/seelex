package core

// teamwork_service.go — Service 侧的**团队看板**读面（view_state 装配端口）。
//
// 口径（契约 docs/arch/team-board-gui-tui-contract.md §3）：
//   - 事实在 seelebridge：计划与审计落 sessionstore 的 moduleTeamwork，作业行活在
//     jobs.Manager 的内存表里。本方法只做**转发**，不解释、不缓存、不做判定——
//     缓存与失效的责任在桥侧（见 seelebridge/runtime_teamwork_board.go）。
//   - 端口是**窄可选**的（contract.TeamworkBoardProjection）：未装配 teamwork 的
//     宿主（测试桩 / 精简宿主）走类型断言失败分支返回 nil，而不是让 RuntimePort
//     长出一个恒返回零值的成员。

import (
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TeamworkBoardViewFor 返回指定会话的团队看板只读投影（无计划 / 未装配 → nil，
// 前端与 TUI 据此整块退场，不留空壳）。
func (service *Service) TeamworkBoardViewFor(sessionID string) *dto.TeamworkBoardView {
	if service == nil || service.Deps.Runtime == nil {
		return nil
	}
	projection, ok := service.Deps.Runtime.(contract.TeamworkBoardProjection)
	if !ok {
		return nil
	}
	return projection.TeamworkBoardSnapshot(sessionID)
}
