package assemble

import (
	"context"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeproject "github.com/RedHuang-0622/seelex/store/project"
)

// SubagentToolObservation 是一条子代理工具事件**加上它的归属主会话**。
//
// 为什么要有这一层（2026-10-19 方向 C）：读数的归属键必须是
// `(ownerSessionID, unitID)`，owner 只能从**执行上下文**取——节点注册时随 ctx
// 绑定的主会话（`SubagentSessions.RegisterFor` 记下的那一格）。事件本体
// （`dto.SubagentToolEvent`）是契约单源，不为了这条链路加字段；归属在本装配层
// 解析后随事件一起交给读侧。
//
// OwnerSessionID 空 = 取不到归属（节点没注册 / 注册时没有会话）：读侧**丢弃并计数**，
// 绝不回落到视图会话——切会话串写（#8/#13）就是这么来的。
type SubagentToolObservation struct {
	OwnerSessionID string
	Event          subagentsession.SubagentToolEvent
}

// RuntimeDeps 是 Runtime 的启动期一次性装配（main.go 装配点一次注入；
// 对应原散装单字段 setter，统一走 Deps 结构——禁止再新增单字段 setter）。
// 运行期可变的投影/权限/plan policy/binding 不在此列（保留独立 setter）。
// 各单字段 setter 仍保留，供测试与细粒度注入使用；生产装配点走 ApplyDeps。
type RuntimeDeps struct {
	// BashDiagnosticObserver bash 工具诊断观察者（nil = 关闭；事件不含命令
	// 文本/参数/输出，可安全用于停滞工具排查）。
	BashDiagnosticObserver toolreg.BashDiagnosticObserver
	// TurnArchiver 压缩轮次原文归档实现（application 层持久化通道）。
	TurnArchiver seelexctx.TurnArchiver
	// ProjectKnowledge 项目级模块语义提供者（nil 关闭 project 块）。
	ProjectKnowledge func() *storeproject.ProjectRecord
	// EventPersister 执行事实持久化钩子（双轨事件的事实轨：
	// event.Sink → store 事件库）。
	EventPersister func(context.Context, frameworkevent.Event) error
	// SubagentToolCallback 子代理工具活动观察者（委托 session.ToolEventState）。
	// 事件与它的**归属主会话**一起交出去（归属由本层从执行上下文解析，见
	// SubagentToolObservation）。
	SubagentToolCallback func(SubagentToolObservation)
	// RoleToolCallback **员工回合**工具活动观察者（subagent 那条的对称面：子代理按
	// NodeID 归到 Plan 节点，员工按角色会话归位）。application 侧把它投影成会话级
	// 事件 teammate.tool.started/completed，前端据此实时更新开着的员工详情。
	RoleToolCallback func(dto.RoleToolActivity)
	// RoleTurnEndCallback 员工回合**收口**观察者：这一轮结束（工具都跑完了）时通知
	// 一次，application 侧据此把该角色会话的工具记录暂存一次性提交给主会话累计
	// （阶梯提交的收口点；空 mainSessionID = 没有归属，读侧丢弃）。
	RoleTurnEndCallback func(mainSessionID, roleSessionID string)
	// SkillRegistry 子代理 skill 目录 actor（nil 关闭 skill 块，降级）。
	SkillRegistry *skill.Registry
	// ScheduledPromptExecutor 周期提示词任务执行器（nil = 禁用 prompt 任务）。
	ScheduledPromptExecutor ScheduledPromptExecutor
	// SchedulerObserver 调度器状态变化通知（main 接 application 投影发布）。
	SchedulerObserver func()
}

// ApplyDeps 一次注入 Runtime 启动期装配（幂等；调用方保证在并发应用工作
// 开始前完成）。nil 字段保持既有值不变（需要显式置 nil 时走对应 setter）。
func (r *Runtime) ApplyDeps(deps RuntimeDeps) {
	if r == nil {
		return
	}
	if deps.BashDiagnosticObserver != nil {
		r.SetBashDiagnosticObserver(deps.BashDiagnosticObserver)
	}
	if deps.TurnArchiver != nil {
		r.SetTurnArchiver(deps.TurnArchiver)
	}
	if deps.ProjectKnowledge != nil {
		r.SetProjectKnowledgeProvider(deps.ProjectKnowledge)
	}
	if deps.EventPersister != nil {
		r.SetEventPersister(deps.EventPersister)
	}
	if deps.SubagentToolCallback != nil {
		r.SetSubagentToolCallback(deps.SubagentToolCallback)
	}
	if deps.RoleToolCallback != nil {
		r.SetRoleToolCallback(deps.RoleToolCallback)
	}
	if deps.RoleTurnEndCallback != nil {
		r.SetRoleTurnEndCallback(deps.RoleTurnEndCallback)
	}
	if deps.SkillRegistry != nil {
		r.SetSkillRegistry(deps.SkillRegistry)
	}
	if deps.ScheduledPromptExecutor != nil {
		r.SetScheduledPromptExecutor(deps.ScheduledPromptExecutor)
	}
	if deps.SchedulerObserver != nil {
		r.SetSchedulerObserver(deps.SchedulerObserver)
	}
}
