package seelebridge

import (
	"context"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	approve "github.com/RedHuang-0622/Seele/workplan/sugar/approve"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
)

// RuntimeDeps 是 Runtime 的启动期一次性装配（main.go 装配点一次注入；
// 对应原散装单字段 setter，统一走 Deps 结构——禁止再新增单字段 setter）。
// 运行期可变的投影/权限/plan policy/binding 不在此列（保留独立 setter）。
// 各单字段 setter 仍保留，供测试与细粒度注入使用；生产装配点走 ApplyDeps。
type RuntimeDeps struct {
	// BashDiagnosticObserver bash 工具诊断观察者（nil = 关闭；事件不含命令
	// 文本/参数/输出，可安全用于停滞工具排查）。
	BashDiagnosticObserver BashDiagnosticObserver
	// TurnArchiver 压缩轮次原文归档实现（application 层持久化通道）。
	TurnArchiver seelexctx.TurnArchiver
	// ProjectKnowledge 项目级模块语义提供者（nil 关闭 project 块）。
	ProjectKnowledge func() *sessionstore.ProjectRecord
	// EventErrorHandler Sink 失败处理器（默认 log.Printf 兜底）。
	EventErrorHandler frameworkevent.ErrorHandler
	// EventPersister 执行事实持久化钩子（双轨事件的事实轨：
	// event.Sink → sessionstore 事件库）。
	EventPersister func(context.Context, frameworkevent.Event) error
	// PlanApprovalGate plan kind:approve/manual 节点审批门控（延迟读取）。
	PlanApprovalGate approve.ApprovalGate
	// PlanNodeCallback 节点/计划状态投影订阅（PlanNodeEvent 回调）。
	PlanNodeCallback func(dto.PlanNodeEvent)
	// SubagentToolCallback 子代理工具活动观察者（委托 session.ToolEventState）。
	SubagentToolCallback func(subagentsession.SubagentToolEvent)
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
	if deps.EventErrorHandler != nil {
		r.SetEventErrorHandler(deps.EventErrorHandler)
	}
	if deps.EventPersister != nil {
		r.SetEventPersister(deps.EventPersister)
	}
	if deps.PlanApprovalGate != nil {
		r.SetPlanApprovalGate(deps.PlanApprovalGate)
	}
	if deps.PlanNodeCallback != nil {
		r.SetPlanNodeCallback(deps.PlanNodeCallback)
	}
	if deps.SubagentToolCallback != nil {
		r.SetSubagentToolCallback(deps.SubagentToolCallback)
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
