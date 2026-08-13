package seelebridge

import (
	"context"
	"fmt"
	"hash/fnv"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
)

// PlanNodeEventChannel 返回 plan 节点事件 channel（CSP：application 消费者
// 串行处理，保序；非阻塞投递，满则丢事件由 Snapshot resync 兜底）。
func (r *Runtime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent {
	if r == nil || r.planExecutor == nil {
		return nil
	}
	return r.planExecutor.PlanNodeEventChannel()
}

// RestorePlan reloads a canonical persisted plan into the runtime plan store.
func (r *Runtime) RestorePlan(ctx context.Context, arguments string) error {
	canonical, err := plan.NormalizePlanLoadArguments(arguments)
	if err != nil {
		return fmt.Errorf("restore plan: normalize persisted plan: %w", err)
	}
	if _, err := r.agentDispatch(ctx, "plan_load", canonical); err != nil {
		return fmt.Errorf("restore plan: %w", err)
	}
	return nil
}

// SetEventErrorHandler 覆盖 Sink 失败处理器（默认 log.Printf 兜底）。
func (r *Runtime) SetEventErrorHandler(handler frameworkevent.ErrorHandler) {
	r.planExecutor.SetEventErrorHandler(handler)
}

// SetEventPersister 安装执行事实持久化钩子（双轨事件的事实轨：
// event.Sink → sessionstore 事件库）。钩子失败经 ErrorHandler 隔离，
// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
func (r *Runtime) SetEventPersister(fn func(context.Context, frameworkevent.Event) error) {
	r.planExecutor.SetEventPersister(fn)
}

// SetPlanApprovalGate 设置 plan kind:approve/manual 节点的审批门控；
// approvalGateNode 在 Run 时读取当前门（延迟读取，可在 plan_load 之后设置）。
func (r *Runtime) SetPlanApprovalGate(gate approve.ApprovalGate) {
	r.planExecutor.SetApprovalGate(gate)
}

// SetPlanBranchBinding freezes context and account-selection inputs for the
// next plan run.
func (r *Runtime) SetPlanBranchBinding(binding dto.PlanBranchBinding) {
	selectedAccountID := r.accounts.Selected()
	if binding.AccountID == "" {
		binding.AccountID = selectedAccountID
	}
	if binding.PrimaryRole == "" {
		binding.PrimaryRole = model.RoleAgent
	}
	if binding.PlanID == "" {
		binding.PlanID = binding.EntryNodeID
	}
	r.planExecutor.SetBinding(binding)
}

// SetPlanNodeCallback 注册节点/计划状态投影订阅：workplan 执行事实
// 经 plan.planEventSink 投影为 dto.PlanNodeEvent（NodeStatus/PlanStatus）后回调。
// 语义与旧框架 NodeResult 回调等价（plan_gate_test 不变）。
func (r *Runtime) SetPlanNodeCallback(cb func(dto.PlanNodeEvent)) {
	r.planExecutor.SetPlanNodeCallback(cb)
}

// SetPlanPolicy updates constraints applied to subsequent plan_load calls.
func (r *Runtime) SetPlanPolicy(policy dto.PlanPolicy) {
	r.planExecutor.SetPolicy(policy)
}

// agentDispatch 统一工具分发入口（agent.DirectDispatch 语义等价）。
func (r *Runtime) agentDispatch(ctx context.Context, name, argsJSON string) (string, error) {
	if r.agt == nil {
		return "", fmt.Errorf("seelebridge: agent is unavailable")
	}
	return r.agt.DirectDispatch(ctx, name, argsJSON)
}

// beginNodeWorktree 为节点创建 worktree（降级返回 nil；语义见
// worktree.WorktreeManager.Begin）。
func (r *Runtime) beginNodeWorktree(scope seenode.NodeScope, nodeID string) *worktree.NodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Begin(scope, nodeID)
}

// branchTraceID 返回分支追踪 ID（planID:branchID 或 traceID:branchID）。
func branchTraceID(binding dto.PlanBranchBinding, branchID string) string {
	if binding.TraceID == "" {
		return branchID
	}
	return binding.TraceID + ":" + branchID
}

// currentAgentFactory 返回当前 plan 子代理工厂（SeelexAgentNode 的读取器）。
func (r *Runtime) currentAgentFactory() node.AgentFactory {
	if r == nil || r.planExecutor == nil {
		return nil
	}
	return r.planExecutor.CurrentAgentFactory()
}

// currentApprovalGate 返回当前审批门（approvalGateNode 的读取器）。
func (r *Runtime) currentApprovalGate() approve.ApprovalGate {
	if r == nil || r.planExecutor == nil {
		return nil
	}
	return r.planExecutor.CurrentApprovalGate()
}

// currentPlanBranchBinding 返回当前 plan 执行分支绑定（planExecutor 持有）。
func (r *Runtime) currentPlanBranchBinding() dto.PlanBranchBinding {
	if r == nil || r.planExecutor == nil {
		return dto.PlanBranchBinding{}
	}
	return r.planExecutor.Binding()
}

// dto.ReplanMetrics returns process-wide replan cost and rejection accounting.
func (r *Runtime) ReplanMetrics() dto.ReplanMetrics {
	if r == nil || r.planExecutor == nil {
		return dto.ReplanMetrics{}
	}
	return r.planExecutor.ReplanMetrics()
}

// finishNodeWorktree 收尾：变基仓库 → 提交判定 → 合并审批 → merge → 清理。
func (r *Runtime) finishNodeWorktree(ctx context.Context, nodeID string, wt *worktree.NodeWorktree) error {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Finish(ctx, nodeID, wt)
}

// forkDeps 把 Runtime 能力面注入 fork 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) forkDeps() fork.Deps {
	return fork.Deps{
		CurrentPlanPolicy:        r.currentPlanPolicy,
		NodeFactory:              r.nodeFactory,
		TaskResolveByKey:         r.ResolveTaskByKey,
		TaskAdd:                  r.TaskAdd,
		TaskSetStatus:            r.TaskSetStatus,
		TaskAttachParticipant:    r.TaskAttachParticipant,
		SubagentTreeRegisterFork: r.subagentTree.RegisterFork,
		SubagentTreeSummaryFor:   r.subagentTree.SummaryFor,
		RunPlan:                  r.planExecutor.RunPlan,
		ForkTimeoutSec:           r.limits.ForkTimeoutSec,
		PlanNodeMaxLoops:         r.limits.PlanNodeMaxLoops,
	}
}

// forkSubagentsHandler 是 fork_subagents 的执行入口（委托 fork.Tool.Handle）。
func (r *Runtime) forkSubagentsHandler(ctx context.Context, argsJSON string) (string, error) {
	if r == nil || r.forkTool == nil {
		return "", fmt.Errorf("fork_subagents: fork tool is not configured")
	}
	return r.forkTool.Handle(ctx, argsJSON)
}

// nodeDeps 把 Runtime 能力面注入 node 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) nodeDeps() seenode.Deps {
	return seenode.Deps{
		CurrentAgentFactory:      r.currentAgentFactory,
		CurrentPlanBranchBinding: r.currentPlanBranchBinding,
		AppendNodePhase:          r.node.AppendPhase,
		BeginNodeWorktree:        r.beginNodeWorktree,
		FinishNodeWorktree:       r.finishNodeWorktree,
		ReleaseNodeWorktree:      r.releaseNodeWorktree,
		RegisterNodeSession:      r.node.RegisterSession,
		UnregisterNodeSession:    r.node.UnregisterSession,
		CompleteSubagentNode:     r.node.CompleteSubagentNode,
		NodeParentEvidence:       r.node.ParentEvidence,
		MergeBackIntoParent:      r.mergeBackIntoParent,
		EnqueueSubagentContext:   r.enqueueSubagentContext,
		NodeBudget:               r.node.Budget,
		NodePromptBlocks:         r.node.PromptBlocks,
		Tracer: func() provider.TraceSource {
			return r.Tracer()
		},
	}
}

// nodeFactory 返回绑定到 Runtime 的 codec.NodeFactory，供 codec.Import/Render 使用。
func (r *Runtime) nodeFactory() codec.NodeFactory[plan.SeelexNodeInput] {
	return plan.NodeFactory(r.nodeFactoryDeps())
}

// nodeFactoryDeps 返回绑定到 Runtime 的跨域构造回调（测试与装配共用）。
func (r *Runtime) nodeFactoryDeps() plan.NodeFactoryDeps {
	return plan.NodeFactoryDeps{
		NewAgentNode: func(spec codec.NodeSpec[plan.SeelexNodeInput]) (node.Node, error) {
			return seenode.NewAgentNode(spec, r.nodeDeps()), nil
		},
		CurrentApprovalGate: r.currentApprovalGate,
		NewSummaryNode: func(spec codec.NodeSpec[plan.SeelexNodeInput]) (node.Node, error) {
			return fork.NewSummaryNode(spec), nil
		},
	}
}

// nodeSessionComponents 构造 Plan 节点子代理会话的公共组件
// （bridge.WithSessionComponents 输入，plan.md §3.1 步骤 5）。
// Agent 由 bridge 强制覆盖为 runtime 的 agent；每节点新建独立 Session
// （工作历史默认隔离）。节点级 PromptBlocks 由 SeelexAgentNode.Run 注入
// ctx，装配器 ScopeAssembler 在每次请求时合并。
func (r *Runtime) nodeSessionComponents() session.SessionComponents {
	return session.SessionComponents{
		Context:   r.nodeContextComponents(),
		Config:    session.SessionConfig{MaxLoops: r.limits.PlanNodeMaxLoops},
		Telemetry: r.hook,
		ModelName: r.model,
	}
}

// nodeSessionID 派生节点会话 ID：以系统提示（节点目标）为种子做稳定 hash；
// 同一节点路径 plan_run 可复现（供未来 checkpoints 定位）；空提示返回空串，
// 让 Session 自动生成不透明 ID。
func (r *Runtime) nodeSessionID(systemPrompt string) string {
	if systemPrompt == "" {
		return ""
	}
	return fmt.Sprintf("node-%x", stableHash(systemPrompt))
}

// registerForkTool 注册 fork_subagents（RegisterBuiltins 内调用）。
func (r *Runtime) registerForkTool() {
	r.RegisterTool("fork_subagents",
		"Fork N isolated subagents in parallel (worktree-isolated) and return their structured outputs."+fork.SubagentsContractDescription,
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subagents": map[string]interface{}{
					"type":        "array",
					"description": "Subagent specs: unique id + natural-language goal.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id":   map[string]interface{}{"type": "string"},
							"goal": map[string]interface{}{"type": "string"},
						},
						"required": []string{"id", "goal"},
					},
				},
				"max_concurrency": map[string]interface{}{"type": "integer", "minimum": 1},
			},
			"required": []string{"subagents"},
		},
		r.forkSubagentsHandler)
}

// releaseNodeWorktree 在节点结束时从注册表移除（成功路径已清理；失败路径
// 保留现场）。
func (r *Runtime) releaseNodeWorktree(nodeID string) {
	if r == nil || r.worktreeMgr == nil {
		return
	}
	r.worktreeMgr.Release(nodeID)
}

// resolvePlanBranchAccount 按 role + planID:branchID 稳定解析分支账号。
func (r *Runtime) resolvePlanBranchAccount(binding dto.PlanBranchBinding, role model.AccountRole, branchID string) (string, error) {
	if binding.AccountID != "" {
		if spec := account.ByName(r.accountSpecList(), binding.AccountID); spec == nil {
			return "", fmt.Errorf("branch %q pins unknown account %q", branchID, binding.AccountID)
		}
		return binding.AccountID, nil
	}
	return account.ResolveForBranch(r.pool, role, binding.PlanID+":"+branchID)
}

// roleForPlanBranch 解析分支账号角色（main/entry → 主账号，其余 → 子代理）。
func roleForPlanBranch(binding dto.PlanBranchBinding, branchID string) model.AccountRole {
	return seenode.RoleForPlanBranch(binding, branchID)
}

// stableHash 返回 seed 的 FNV-1a 32 位稳定哈希。
func stableHash(seed string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return hash.Sum32()
}
func (r *Runtime) currentPlanPolicy() dto.PlanPolicy {
	if r == nil || r.planExecutor == nil {
		return dto.PlanPolicy{}
	}
	return r.planExecutor.Policy()
}
