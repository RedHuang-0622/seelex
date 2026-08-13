package seelebridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
)

// ports.go 承载 Runtime 对 application/contract 端口的实现方法（组合根公开面）。
// 域实现已下沉子包；本文件只保留委托方法，DTO 统一走 application/contract/dto。

// mainAgentNodeID 是子代理树的合成根节点 ID（常量本体在 internal/model）。
const mainAgentNodeID = model.MainAgentNodeID

// ──── task 端口 ────────────────────────────────────────────────────────

// TaskSnapshot 返回 task 注册表只读快照（worktable 投影数据源）。
func (r *Runtime) TaskSnapshot() []dto.TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.tasks.Snapshot()
}

// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (r *Runtime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.Add(spec)
}

// ResolveTaskByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (r *Runtime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.ResolveByKey(key)
}

// TaskSetStatus 更新 task 状态（retry 自增计数；运行中保留计数）。
func (r *Runtime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.SetStatus(id, status, evidence)
}

// TaskAttachParticipant 把子代理挂为 task 参与者（幂等）。
func (r *Runtime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.AttachParticipant(id, participant)
}

// TaskAppendTrace 追加 task 打点。
func (r *Runtime) TaskAppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.AppendTrace(id, point)
}

// SwitchSessionTasks 会话级 task 隔离：切换会话时整体替换注册表。
func (r *Runtime) SwitchSessionTasks(records []dto.TaskRecord) {
	if r == nil || r.tasks == nil {
		return
	}
	_ = r.tasks.ReplaceAll(records)
}

// TaskChangedChannel 返回 task.changed 输出 channel（CSP：变更即投递）。
func (r *Runtime) TaskChangedChannel() <-chan dto.TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.tasks.TaskChanged()
}

// RegisterTaskTerminalTools 把终态工具 provider 注册进工具注册表（幂等）。
func (r *Runtime) RegisterTaskTerminalTools(handler task.TaskTerminalHandler) {
	state := r.registry
	if state == nil || state.Registry == nil {
		return
	}
	_ = state.Registry.Unregister("seelex-task-terminal")
	if err := state.Registry.Register(task.NewTaskTerminalProvider(handler)); err != nil {
		return
	}
}

// ──── plan 端口 ────────────────────────────────────────────────────────

// PrepareReplan atomically replaces a failed WorkPlan with a recovery plan.
// It only plans; it never invokes plan_run or retries side effects.
func (r *Runtime) PrepareReplan(ctx context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error) {
	if r == nil || r.planExecutor == nil {
		return dto.PlanPreflight{}, fmt.Errorf("plan replan: plan executor is unavailable")
	}
	return r.planExecutor.PrepareReplan(ctx, request)
}

// ──── 子代理树端口 ─────────────────────────────────────────────────────

// ClearSubagentTree 清空子代理树（GUI"清空"按钮入口）。
func (r *Runtime) ClearSubagentTree() error {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Clear()
}

// SubagentTreeEvents 返回子代理树生命周期信号 channel（CSP 消费者）。
func (r *Runtime) SubagentTreeEvents() <-chan struct{} {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Events()
}

// SubAgentTree 返回子代理树的只读投影（根 = 主代理）。
func (r *Runtime) SubAgentTree() []dto.SubAgentTreeNode {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Projection()
}

// SetSubagentToolCallback 注入子代理工具活动观察者（委托 session.ToolEventState）。
func (r *Runtime) SetSubagentToolCallback(callback func(subagentsession.SubagentToolEvent)) {
	if r == nil || r.toolEvents == nil {
		return
	}
	r.toolEvents.SetCallback(callback)
}
