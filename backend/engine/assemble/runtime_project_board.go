package assemble

// runtime_project_board.go 是**项目看板窄可选端口的宿主转发面**。
//
// 落点沿革（P21-11「域方法随类型走」）：取数判定（会话号归一、键完整性、读失败与空计划的
// 处理）搬进看板域 [`kernel/board`](../../../kernel/board/README.md) 的
// `TeamworkPlanFor`；这里只留**两格宿主读数**的接线——teamwork 后端句柄（`r.teamworkBackend`，
// 装配期注入、可能未装配）与它的 `KeyFor` / `Store.ReadPlan`。
//
// 生态位：application/assembly 的看板投影需要"某会话的 teamwork 计划"，但它不该认识 Runtime 的接口
// 形状——所以那一侧只声明窄接口（`board.TeamworkPlanSource`），本文件让 Runtime 满足它。

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/kernel/board"
	"github.com/RedHuang-0622/seelex/store"
)

// errTeamworkPlanUnavailable 是"teamwork 后端未装配"在计划读口上的报错（与"读失败"同解：
// 域包对两者只做同一件事——回 (零值, false)）。
var errTeamworkPlanUnavailable = errors.New("teamwork: 后端未装配")

// TeamworkPlanFor 实现 `board.TeamworkPlanSource`：返回该会话的 teamwork 计划。
//
// 判定在域包（`board.TeamworkPlanFor`）；本方法只把"当前装配到的 teamwork 后端"接成两格读数。
func (r *Runtime) TeamworkPlanFor(sessionID string) (store.TeamworkPlan, bool) {
	if r == nil {
		return store.TeamworkPlan{}, false
	}
	return board.TeamworkPlanFor(sessionID, board.TeamworkPlanDeps{
		KeyFor:   r.teamworkKeyFor,
		ReadPlan: r.readTeamworkPlan,
	})
}

// readTeamworkPlan 读当前装配的 teamwork 后端的计划读口（未装配 ⇒ 报错，与"读失败"同解）。
func (r *Runtime) readTeamworkPlan(ctx context.Context, key store.Key) (store.TeamworkPlan, error) {
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.Store == nil {
		return store.TeamworkPlan{}, errTeamworkPlanUnavailable
	}
	return backend.Store.ReadPlan(ctx, key)
}

// 编译期钉子：*Runtime 必须满足 `board.TeamworkPlanSource` 的**形状**（同名同签名）。
//
// 依赖方向是 assemble → kernel/board（不反向），所以本文件**直接**拿得到那个接口名——
// 形状由一条断言钉住，而不是靠两侧各写一份结构体字面量来表达同一件事。
var _ board.TeamworkPlanSource = (*Runtime)(nil)
