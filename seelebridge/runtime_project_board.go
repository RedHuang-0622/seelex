package seelebridge

// runtime_project_board.go — 项目看板的**窄可选端口**（M3 装配侧）。
//
// 生态位：application/core 的看板投影需要"某会话的 teamwork 计划"，但它不该认识
// Runtime 的接口形状——所以那一侧只声明一个窄接口（`core.TeamworkPlanSource`），
// 本文件让 Runtime 满足它（`TeamworkPlanFor`）。装配根把
// `core.ProjectBoardDeps.TeamPlanFor = app.ProjectBoardTeamPlanFor` 接上即可。
//
// 取数口径（与团队看板 `TeamworkBoardSnapshot` 同源，不新造一条读法）：
//   - 作用域键由 `TeamworkBackend.KeyFor` 解析（会话 → 所属项目 + 会话）；
//   - 计划读 `backend.Store.ReadPlan`（moduleTeamwork head，整份替换型内容）；
//   - **不共享** `teamworkBoardCache`：那份缓存是团队看板自己的加速层（原地写入、
//     随 team_* 失效），跨 goroutine 从它读会引入本文件不该引入的一致性问题。
//     投影侧的取数发生在去抖后的 flush goroutine 上，每批一次小文件读不构成瓶颈。
//
// **不**判"计划是否已收口"：收口（`State.State == closed`）是投影要搬运的**事实**
// （看板行的 `team_state` 一格），不是"要不要出图"的判据——那是团队看板 GUI 的口径
// （收口后整块退场）。两者读同一份计划，各按自己的语义用。

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TeamworkPlanFor 实现 `core.TeamworkPlanSource`：返回该会话的 teamwork 计划。
//
// 未装配 teamwork 面 / 会话解析不出作用域 / 该会话没有计划 / 读失败 ⇒ (零值, false)。
// 读失败与"没有计划"同解，是因为调用方（看板投影）对两者只做同一件事：整块退场
// （I6：空则退场，不建文件、不留空壳）。
func (r *Runtime) TeamworkPlanFor(sessionID string) (sessionstore.TeamworkPlan, bool) {
	if r == nil {
		return sessionstore.TeamworkPlan{}, false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionstore.TeamworkPlan{}, false
	}
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.Store == nil || backend.KeyFor == nil {
		return sessionstore.TeamworkPlan{}, false
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok {
		return sessionstore.TeamworkPlan{}, false
	}
	plan, err := backend.Store.ReadPlan(context.Background(), key)
	if err != nil {
		return sessionstore.TeamworkPlan{}, false
	}
	return plan, true
}
