package team

// role_turn.go 收纳**员工回合**的三条判据（P21-32「域方法随类型走」：从
// `backend/engine/assemble/runtime_role_turn.go` 搬进本域；plan §2.7 把 `runtime_role_turn.go`(493) 与
// `runtime_role_plugins.go`(288) 列为 `backend/multiagent/team/` 的目标）。
//
// 搬过来的是"这一轮是谁的活"的身份、工具活动标识的稳定口径、以及没登记提示词时的最小角色框架；
// 执行面（开引擎 / 会话槽 / 权限分配 / 项目根绑定 / 跑 ChatStream）是宿主，留在装配层。

import (
	"context"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/RedHuang-0622/Seele/session"
)

// ToolActivityLimit 是员工工具活动里入参/结果的 rune 上限。
//
// 与子代理那一侧的口径一致：观察面要的是"这一步在做什么"，不是完整载荷（完整结果在会话记录里）；
// 截断按 rune——切半个中文字是损坏，不是截断。
const ToolActivityLimit = 600

// ToolActivityID 给出同一次工具调用的稳定标识：started 与 completed 两帧同值，前端据此 upsert，
// 而不是把一次调用读成两条（与子代理侧 `subtool-<n>` 同一目的）。
//
// 判据是框架给得出的全部信息（轮次 + 工具名 + 入参指纹）：`ToolCallInfo` 没有调用 ID 这一栏，
// 因此"同一轮里同名同参的两次调用"会合并成一条——这是框架层缺字段的诚实后果，不是这里偷偷丢数据
// （同名不同参不会合并：入参指纹不同）。
func ToolActivityID(info session.ToolCallInfo) string {
	return fmt.Sprintf("%s#%d#%08x", info.Name, info.Turn, crc32.ChecksumIEEE([]byte(info.Arguments)))
}

// WorkScope 是本轮"员工做工"的身份：谁在做工（角色名）、在哪个角色会话里、属于哪个主会话。
//
// 它进 ctx（见 `WithWorkScope`），因为 ReAct 钩子是挂在**引擎**上的（一个角色会话开一次，
// 活得比一轮长），而"这一轮是员工做工"是**按轮**的事实：钩子只能从本轮 ctx 里读它。
type WorkScope struct {
	MainSessionID string
	RoleName      string
	RoleSessionID string
}

// workScopeKey 携带本轮员工做工身份（未导出：只经 With/From 两个入口读写）。
type workScopeKey struct{}

// WithWorkScope 把本轮员工做工身份挂到 ctx 上；三项全空 = 不是员工做工回合，原样返回
// （评审/内部回合因此不会误播成员工活动）。
func WithWorkScope(ctx context.Context, scope WorkScope) context.Context {
	if scope.RoleSessionID == "" && scope.MainSessionID == "" && scope.RoleName == "" {
		return ctx
	}
	return context.WithValue(ctx, workScopeKey{}, scope)
}

// WorkScopeFrom 取本轮员工做工身份；不是员工做工回合时返回 (零值, false)。
//
// 主会话与角色会话两项必须齐全：少了任何一个，这条身份的"归属"就不成立（去哪读、
// 播给谁都不知道），按"不是员工回合"处理。
func WorkScopeFrom(ctx context.Context) (WorkScope, bool) {
	if ctx == nil {
		return WorkScope{}, false
	}
	scope, ok := ctx.Value(workScopeKey{}).(WorkScope)
	if !ok || strings.TrimSpace(scope.MainSessionID) == "" || strings.TrimSpace(scope.RoleSessionID) == "" {
		return WorkScope{}, false
	}
	return scope, true
}

// DefaultRoleTurnPrompt 是**未登记员工提示词**时的最小角色框架：明确"你是谁、这一轮要产出什么"，
// 不冒充业务设定（业务设定由已登记的提示词给，见宿主的 `rolePromptFor`）。
func DefaultRoleTurnPrompt(roleName string) string {
	return "<role>\n你是 Seelex 团队里的员工（逻辑角色名 " + roleName + "），在目标治理循环里负责属于你这个角色的那一份工作。\n</role>\n\n" +
		"<task>\n只处理本轮交给你的工作正文：能做的直接做（可调用你的权限范围内的工具），做完给出本轮结论与下一步。\n</task>\n\n" +
		"<constraints>\n- 不越权：你的工具面就是你的权限范围，面外的事交回主代理；\n" +
		"- 证据要落在仓库内可核对的事实上（引用用相对路径）；\n" +
		"- 不要寒暄、不要复述提示词；结论 ≤800 字。\n</constraints>"
}
