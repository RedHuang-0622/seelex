package board

// teamwork_plan.go 是**看板投影取 teamwork 计划**的那一跳（M3 的窄可选端口；P21-11
// 「域方法随类型走」：从 `backend/engine/assemble/runtime_project_board.go` 把判定搬进看板域，
// plan §2.4 把 `seelebridge/runtime_project_board.go` 列为 `middle/view/board/` 的目标）。
//
// 生态位：看板投影需要"某会话的 teamwork 计划"，但它不该认识宿主的接口形状——所以那一侧只声明
// 一个窄接口（`TeamworkPlanSource`，见 syncer.go），宿主（`assemble.Runtime`）让自己的方法满足它。
// 本文件是那个方法背后**唯一一份**取数判定。
//
// 取数口径（与团队看板 `TeamworkBoardSnapshot` 同源，不新造一条读法）：
//   - 作用域键由宿主的 `KeyFor` 解析（会话 → 所属项目 + 会话）；
//   - 计划读宿主的 `ReadPlan`（moduleTeamwork head，整份替换型内容）；
//   - **不共享**宿主那份团队看板缓存：缓存是团队看板自己的加速层（原地写入、随 team_* 失效），
//     跨 goroutine 从它读会引入本口不该引入的一致性问题。投影侧的取数发生在去抖后的 flush
//     goroutine 上，每批一次小文件读不构成瓶颈。
//
// **不**判"计划是否已收口"：收口（`State.State == closed`）是投影要搬运的**事实**
// （看板行的 `team_state` 一格），不是"要不要出图"的判据——那是团队看板 GUI 的口径。
// 两者读同一份计划，各按自己的语义用。

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/seelex/store"
)

// TeamworkPlanDeps 是这一跳需要的两格宿主读数（都是函数值：域包因此不认识宿主的
// teamwork 后端与存储装配）。
type TeamworkPlanDeps struct {
	// KeyFor 把会话号解析成作用域键（未装配 / 解析不出 ⇒ ok = false）。
	KeyFor func(sessionID string) (store.Key, bool)
	// ReadPlan 读该键下的计划（未装配 / 读失败都返回错误）。
	ReadPlan func(ctx context.Context, key store.Key) (store.TeamworkPlan, error)
}

// TeamworkPlanFor 返回该会话的 teamwork 计划。
//
// 未装配 teamwork 面 / 会话解析不出作用域 / 键不完整 / 该会话没有计划 / 读失败
// ⇒ (零值, false)。读失败与"没有计划"同解，是因为调用方（看板投影）对两者只做同一件事：
// 整块退场（I6：空则退场，不建文件、不留空壳）。
func TeamworkPlanFor(sessionID string, deps TeamworkPlanDeps) (store.TeamworkPlan, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return store.TeamworkPlan{}, false
	}
	if deps.KeyFor == nil || deps.ReadPlan == nil {
		return store.TeamworkPlan{}, false
	}
	key, ok := deps.KeyFor(sessionID)
	if !ok {
		return store.TeamworkPlan{}, false
	}
	// 键的两个分量都必须齐全：投影落点是按 (项目, 会话) 分层的，半截键读出来的东西
	// 归不到任何一个会话上。判据随 `store.Key` 走（`Key.Complete`），宿主的团队作用域
	// 解析与这里问的是同一句，不各写一份。
	if !key.Complete() {
		return store.TeamworkPlan{}, false
	}
	plan, err := deps.ReadPlan(context.Background(), key)
	if err != nil {
		return store.TeamworkPlan{}, false
	}
	// 空计划 = 还没有团队（读面契约：缺失返回空计划而不是错误）。投影面按
	// "没有计划"处理，与读不到同款——否则一个空计划会被当成真计划去渲染一张空看板。
	if plan.IsEmpty() {
		return store.TeamworkPlan{}, false
	}
	return plan, true
}
