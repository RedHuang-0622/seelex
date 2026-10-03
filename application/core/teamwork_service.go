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

// TeammateSessionLiveFor 返回**当前 teammate 会话**的实时只读投影（"这件事的会话此刻在
// 说什么"）。未装配 / 会话不在本进程 → Running=false 的视图（不是错误：那不是"读失败"，
// 而是"这个执行面不在本进程里"，调用方要能如实说出来）。
//
// 为什么要有这一面（2026-10-04 用户口径：查看 teammates 的会话看到的"全是历史会话"）：
// 员工的角色会话落盘、可回读；而一个 Work Item 自己的会话是**进程内执行面**（刻意不接
// DurableHistory），正文不在会话库里——从存储读只会读到主会话的历史。
func (service *Service) TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView {
	if service == nil || service.Deps.Runtime == nil {
		return dto.TeammateSessionLiveView{SessionID: sessionID}
	}
	projection, ok := service.Deps.Runtime.(contract.TeammateSessionProjection)
	if !ok {
		return dto.TeammateSessionLiveView{SessionID: sessionID}
	}
	return projection.TeammateSessionLive(sessionID)
}
