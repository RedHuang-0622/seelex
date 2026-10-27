package teamwork

// board.go — 团队看板读面的**转发 + 空视图口径**（原 `application/assembly/teamwork_service.go`，
// M5-R8c 按 plan §2.7 归位多代理板块）。
//
// 事实不在这里：计划与审计落 store 的 moduleTeamwork，作业行活在 jobs.Manager 的内存
// 表里；本文件只做**转发**——不解释、不缓存、不做判定（缓存与失效的责任在装配侧，见
// `backend/engine/assemble/runtime_teamwork_board.go`）。
//
// 端口是**窄可选**的（`contract.TeamworkBoardProjection` / `contract.TeammateSessionProjection`）：
// 未装配 teamwork 的宿主（测试桩、精简宿主）走类型断言失败分支返回零值视图，
// 而不是让 `contract.RuntimePort` 长出一个恒返回零值的成员。
//
// 为什么 `Source` 用 `any` 而不是 `contract.RuntimePort`：本读面**只**关心那两个窄端口的
// 断言结果，把参数写成 `RuntimePort` 会逼测试桩实现整套宿主面（39 个方法）才能覆盖
// "已装配但不支持该投影"这一支——那正是本读面要处理的最常见分支。断言在 `any` 上
// 与在 `RuntimePort` 上语义相同（都按动态类型判定）。

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// BoardReader 读一个会话的团队看板与 teammate 会话 live 读数。
// 零值可用：`source` 为 nil 即"未装配"，三条读法各自返回自己的空视图口径。
type BoardReader struct{ source any }

// NewBoardReader 绑定宿主运行面（传 `service.Deps.Runtime` 这类宿主面接口值）。
func NewBoardReader(source any) BoardReader { return BoardReader{source: source} }

// BoardViewFor 返回指定会话的团队看板只读投影（无计划 / 未装配 → nil，
// 前端与 TUI 据此整块退场，不留空壳）。
func (reader BoardReader) BoardViewFor(sessionID string) *dto.TeamworkBoardView {
	projection, ok := reader.projections()
	if !ok {
		return nil
	}
	return projection.TeamworkBoardSnapshot(sessionID)
}

// SessionLiveFor 返回**当前 teammate 会话**的实时只读投影（"这件事的会话此刻在
// 说什么"）。未装配 / 会话不在本进程 → Running=false 的视图（不是错误：那不是"读失败"，
// 而是"这个执行面不在本进程里"，调用方要能如实说出来）。
//
// 为什么要有这一面（2026-10-04 用户口径：查看 teammates 的会话看到的"全是历史会话"）：
// 员工的角色会话落盘、可回读；而一个 Work Item 自己的会话是**进程内执行面**（刻意不接
// DurableHistory），正文不在会话库里——从存储读只会读到主会话的历史。
func (reader BoardReader) SessionLiveFor(sessionID string) dto.TeammateSessionLiveView {
	projection, ok := reader.liveProjections()
	if !ok {
		return dto.TeammateSessionLiveView{SessionID: sessionID}
	}
	return projection.TeammateSessionLive(sessionID)
}

// SessionLivePageFor 返回**当前 teammate 会话**实时读数的分页一页（有界窗口 +
// 分页：把"只有尾巴"改成"翻得动"）。
//
// 与 SessionLiveFor 是**同一份读数**的两种取法（判据只有一份，在装配侧）：
// offset 从该会话可投影的第一条算起（0 = 最旧）；未装配 / 会话不在本进程 →
// Running=false 的空页（不是错误，理由同上）。
func (reader BoardReader) SessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	projection, ok := reader.liveProjections()
	if !ok {
		return dto.TeammateSessionLiveView{SessionID: sessionID, Offset: offset, Limit: limit}
	}
	return projection.TeammateSessionLivePage(sessionID, offset, limit)
}

// boardProjections 是"能回答团队看板"的宿主面；未装配 → 取不到。
func (reader BoardReader) projections() (contract.TeamworkBoardProjection, bool) {
	projection, ok := reader.source.(contract.TeamworkBoardProjection)
	return projection, ok
}

// liveProjections 是"能回答 teammate 会话 live 读数"的宿主面；未装配 → 取不到。
func (reader BoardReader) liveProjections() (contract.TeammateSessionProjection, bool) {
	projection, ok := reader.source.(contract.TeammateSessionProjection)
	return projection, ok
}
