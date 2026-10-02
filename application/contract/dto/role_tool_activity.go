package dto

import "time"

// role_tool_activity.go — **员工回合**（teammate 在做工）里一次工具调用的实时活动投影。
//
// 生态位：subagent 侧已有同一件事的那一份（`SubagentToolEvent`，见 subagent_live.go：
// 子代理工具调用 → `subagent.tool.started/completed` 增量 → 前端详情实时更新）。员工
// （teammate）侧此前**没有**：角色回合的 ReAct 钩子只把工具步骤送进 goal 域的
// TLStep sink（评审过程），而那个 sink 只有 ADVISOR 回合挂得上——于是 teammate 在做工
// 时，前端一无所知，只能等这一轮跑完（leader 写里程碑那一刻）才看得到结果。
//
// 本结构补的就是这一格：同一条钩子对**员工回合**也发一份活动投影，经装配根送到
// application（`Service.HandleRoleToolActivity`）→ 会话级事件
// `teammate.tool.started/completed` → 前端热更新开着的员工详情。
//
// 与 `SubagentToolEvent` 的差别只有身份那一栏：子代理按 **NodeID** 归到 Plan 节点，
// 员工按 **(主会话, 角色名, 角色会话)** 归到角色会话——两者是同一件事的两面，不合并成
// 一个结构（合并会让每个消费方都要先判断"这条到底是哪个身份"，而字段名会说谎）。
//
// 有界：Arguments / Result / Error 由发布方按 rune 截断（切半个中文字 = 损坏，不是截断）。
type RoleToolActivity struct {
	// ID 是同一次工具调用的稳定标识（started 与 completed 两帧同值，供前端 upsert）。
	ID string `json:"id"`
	// MainSessionID 是这位 teammate 所属的**主会话**（事件路由键；空值不发布）。
	MainSessionID string `json:"main_session_id,omitempty"`
	// RoleName 是逻辑角色名（emp_<role> 的主体名），RoleSessionID 是它的角色会话。
	RoleName      string `json:"role_name,omitempty"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	// Name 是工具名；Arguments 是原样入参（已按 rune 截断）。
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// Status 取 running | success | error（与 SubagentToolEvent 同词表）。
	Status string `json:"status,omitempty"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// StartedAt 是这一轮工具调用的开始时刻；Duration 只在 completed 帧上有意义。
	StartedAt time.Time     `json:"started_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	// Turn 是本轮 ReAct 的轮次序号（同一次工具调用的 started/completed 同值）。
	Turn int `json:"turn,omitempty"`
}
