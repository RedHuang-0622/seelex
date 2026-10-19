package assemble

import (
	"context"
	"fmt"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	frameworknode "github.com/RedHuang-0622/Seele/workplan/core/node"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/provider"
	"github.com/RedHuang-0622/seelex/backend/engine/obs"
	"github.com/RedHuang-0622/seelex/backend/tool/fork"
	seenode "github.com/RedHuang-0622/seelex/backend/tool/node"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/backend/tool/worktree"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
	seetelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	"github.com/RedHuang-0622/seelex/store"
)

// SetEventPersister 安装执行事实持久化钩子（事件库的事实轨）。
// 钩子失败由下游钩子自行隔离，不破坏工作单元控制流。
func (r *Runtime) SetEventPersister(fn func(context.Context, frameworkevent.Event) error) {
	// 持久化前为主会话事件补 session_id 关联（判据与实现在观测域
	// backend/engine/obs/event_correlation.go）。
	fn = obs.CorrelateMainSessionID(r.MainSessionID, fn)
	r.eventPersisterMu.Lock()
	r.eventPersister = fn
	r.eventPersisterMu.Unlock()
	// 统一事件库：B 类摘要与 A 类事实同一 store 事件库（同库不双写）。
	if r.summaryLog != nil {
		r.summaryLog.SetPersister(fn)
	}
}

// currentEventPersister 返回装配期注入的事实持久化钩子（可能为 nil = 宿主
// 没接 store 事件库；此时作业事件流静默不落库，不报错）。
func (r *Runtime) currentEventPersister() func(context.Context, frameworkevent.Event) error {
	if r == nil {
		return nil
	}
	r.eventPersisterMu.Lock()
	defer r.eventPersisterMu.Unlock()
	return r.eventPersister
}

// SetSubagentPolicy 写入子代理执行策略默认槽（effort 档位决定 fork 数量/并发
// 上限；运行期可刷新）。
func (r *Runtime) SetSubagentPolicy(policy dto.SubagentPolicy) {
	if r == nil {
		return
	}
	r.subagentPolicyMu.Lock()
	r.subagentPolicy = policy
	r.subagentPolicyMu.Unlock()
}

// SetSubagentPolicyFor 按会话写入子代理执行策略槽（会话 effort 同步入口；
// 后台会话读取自己的槽，不继承进程默认槽）。
func (r *Runtime) SetSubagentPolicyFor(sessionID string, policy dto.SubagentPolicy) {
	if r == nil {
		return
	}
	r.subagentPolicyMu.Lock()
	defer r.subagentPolicyMu.Unlock()
	if r.subagentPolicyBySID == nil {
		r.subagentPolicyBySID = make(map[string]dto.SubagentPolicy)
	}
	r.subagentPolicyBySID[sessionID] = policy
}

// currentSubagentPolicy 返回执行 ctx 所属会话的策略；未建槽时回退默认槽。
func (r *Runtime) currentSubagentPolicy(ctx context.Context) dto.SubagentPolicy {
	if r == nil {
		return dto.SubagentPolicy{}
	}
	r.subagentPolicyMu.RLock()
	defer r.subagentPolicyMu.RUnlock()
	if sessionID := seetelemetry.SessionIDFromContext(ctx); sessionID != "" {
		if policy, ok := r.subagentPolicyBySID[sessionID]; ok {
			return policy
		}
	}
	return r.subagentPolicy
}

// agentDispatch 统一工具分发入口（agent.DirectDispatch 语义等价）。
func (r *Runtime) agentDispatch(ctx context.Context, name, argsJSON string) (string, error) {
	if r.agt == nil {
		return "", fmt.Errorf("seelex: agent is unavailable")
	}
	return r.agt.DirectDispatch(ctx, name, argsJSON)
}

// beginNodeWorktree 为节点创建 worktree（降级返回 nil；语义见
// worktree.WorktreeManager.Begin）。
func (r *Runtime) beginNodeWorktree(scope dto.SceneScope, nodeID string) *worktree.NodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	wt := r.worktreeMgr.Begin(scope, nodeID)
	if wt != nil && r.subagentSessions != nil {
		r.subagentSessions.NoteWorktree(nodeID, store.NodeWorktreeRecord{
			Path: wt.Path, Branch: wt.Branch, BaseCommit: wt.BaseCommit, MainBranch: wt.MainBranch,
		})
	}
	return wt
}

// currentAgentFactory 返回当前子代理工厂（AgentNode 的读取器）。
func (r *Runtime) currentAgentFactory() frameworknode.AgentFactory {
	if r == nil {
		return nil
	}
	return r.agentFactory
}

// currentNodeSceneScope 解析节点执行作用域。Plan 执行器退役后，节点只有
// 两类：fork 的单条子代理与 workunit 的后台工作单元，均按 subagent 角色执行；
// worktree 根由 BeginNodeWorktree 回填。
func (r *Runtime) currentNodeSceneScope(sceneID string) dto.SceneScope {
	return dto.SceneScope{
		SceneID:  sceneID,
		Role:     model.RoleSubAgent,
		BranchID: sceneID,
	}
}

// finishNodeWorktree 收尾：变基仓库 → 提交判定 → 合并 → 清理。
func (r *Runtime) finishNodeWorktree(ctx context.Context, nodeID string, wt *worktree.NodeWorktree) error {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Finish(ctx, nodeID, wt)
}

// forkDeps 把 Runtime 能力面注入 fork 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) forkDeps() fork.Deps {
	return fork.Deps{
		CurrentSubagentPolicy:    r.currentSubagentPolicy,
		RunSubagent:              r.runForkSubagent,
		TaskResolveByKeyFor:      r.ResolveTaskByKeyFor,
		TaskAddFor:               r.TaskAddFor,
		TaskSetStatusFor:         r.TaskSetStatusFor,
		TaskAttachParticipant:    r.TaskAttachParticipant,
		SubagentTreeRegisterFork: r.subagentTree.RegisterFork,
		SubagentTreeSummaryFor:   r.subagentTree.SummaryFor,
		ForkTimeoutSec:           r.limits.ForkTimeoutSec,
		PlanNodeMaxLoops:         r.limits.PlanNodeMaxLoops,
		LoopLimitCeiling:         r.limits.InputLoopLimit,
		Jobs:                     r.subagentJobs(),
	}
}

// runForkSubagent 直接执行一条 fork 子代理：按一份 spec 起一个子代理会话并
// 收尾，不经过 DAG/依赖/屏障。执行体仍是 AgentNode（worktree、作用域、
// prompt blocks、merge-back 全在原实现里）。
func (r *Runtime) runForkSubagent(ctx context.Context, id, goal, taskID string, maxLoops int) (string, error) {
	spec := codec.NodeSpec[seenode.SeelexNodeInput]{
		ID:   id,
		Kind: "agent",
		Input: seenode.SeelexNodeInput{
			ID: id, Input: goal, TaskID: taskID,
			Budget: &seenode.NodeBudgetInput{MaxLoops: maxLoops},
		},
	}
	agentNode := seenode.NewAgentNode(spec, r.nodeDeps())
	return agentNode.Run(ctx, nil)
}

// subagentJobsAdapter 把作业域暴露给 fork 域的 SubagentJobs 端口。
type subagentJobsAdapter struct{ runtime *Runtime }

// Add 登记一条子代理作业并挂取消口。
func (a subagentJobsAdapter) Add(spec fork.SubagentJobSpec, cancel context.CancelFunc) (string, error) {
	router := a.router()
	if router == nil {
		return "", fmt.Errorf("子代理作业面不可用（工具路由未装配）；子代理派发只走作业面，不退化成阻塞调用")
	}
	return router.AddSubagentJob(toolreg.JobSpec{
		Kind:      toolreg.JobKindSubagent,
		SessionID: spec.SessionID,
		Command:   spec.Goal,
		Title:     "子代理 " + spec.ID + ": " + spec.Goal,
	}, cancel)
}

// Note 追加子代理的已产出内容（kill 后仍可 fetch）。
func (a subagentJobsAdapter) Note(handle, text string) {
	if router := a.router(); router != nil {
		_ = router.NoteJob(handle, text)
	}
}

// Complete 合成终态（运行体系被动 done）。
func (a subagentJobsAdapter) Complete(handle string, state dto.AsyncState) {
	if router := a.router(); router != nil {
		router.CompleteJob(handle, state)
	}
}

// router 延迟解析工具路由：fork 域在 NewRuntime 构造期就拿到这个端口（那时
// 工具路由还没装配），必须在调用时现取。
func (a subagentJobsAdapter) router() *toolreg.Router {
	if a.runtime == nil {
		return nil
	}
	return a.runtime.scopedTools
}

// subagentJobs 返回子代理作业端口（延迟解析）。
func (r *Runtime) subagentJobs() fork.SubagentJobs {
	if r == nil {
		return nil
	}
	return subagentJobsAdapter{runtime: r}
}

// forkSubagentsHandler 是 fork_subagents 的执行入口（委托 fork.Tool.Handle）。
func (r *Runtime) forkSubagentsHandler(ctx context.Context, argsJSON string) (string, error) {
	if r == nil || r.forkTool == nil {
		return "", fmt.Errorf("fork_subagents: fork tool is not configured")
	}
	sessionID := seetelemetry.SessionIDFromContext(ctx)
	r.ForkBegin(sessionID)
	defer r.ForkEnd(sessionID)
	return r.forkTool.Handle(ctx, argsJSON)
}

// nodeDeps 把 Runtime 能力面注入 node 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) nodeDeps() seenode.Deps {
	return seenode.Deps{
		CurrentAgentFactory: r.currentAgentFactory,
		CurrentSceneScope:   r.currentNodeSceneScope,
		// 生命周期（建现场 / 收尾 / 释放）只有一份：这三个回调都驱动 subagent 层的
		// 工作单元（父实现见 workunit_parent.go），现场的唯一建法与收尾仍在既有实现里
		// 被父调用。
		BeginNodeWorktree:   r.beginNodeUnit,
		FinishNodeWorktree:  r.finishNodeUnit,
		ReleaseNodeWorktree: r.releaseNodeWorktree,
		// 注册/注销同时维护**归属登记表**：工具读数的归属键在执行上下文里绑定一次
		// （mainSessionID 来自 ctx），热路径只读这一次登记（见 subagentOwnerSession）。
		RegisterNodeSession: func(mainSessionID, nodeID string, sess *session.Session, goal string) {
			r.noteSubagentOwner(nodeID, mainSessionID)
			r.node.RegisterSession(mainSessionID, nodeID, sess, goal)
		},
		UnregisterNodeSession: func(nodeID string) {
			r.forgetSubagentOwner(nodeID)
			r.node.UnregisterSession(nodeID)
		},
		CompleteSubagentNode: r.node.CompleteSubagentNode,
		NodeParentEvidence:   r.node.ParentEvidence,
		MergeBackIntoParent:  r.mergeBackIntoParent,
		RecordNodeStage:      r.node.RecordStage,
		RecordNodeResult:     r.node.RecordResult,
		RecordNodeAssistant:  r.recordNodeAssistant,
		NodeBudget:           r.node.Budget,
		NodePromptBlocks:     r.node.PromptBlocks,
		Tracer: func() provider.TraceSource {
			return r.Tracer()
		},
	}
}

// nodeSessionComponents 构造子代理会话的公共组件（bridge.WithSessionComponents
// 输入）。Agent 由 bridge 强制覆盖为 runtime 的 agent；每条子代理新建独立
// Session（工作历史默认隔离）。节点级 PromptBlocks 由 AgentNode.Run 注入 ctx，
// 装配器 ScopeAssembler 在每次请求时合并。
func (r *Runtime) nodeSessionComponents() session.SessionComponents {
	return session.SessionComponents{
		Context:   r.nodeContextComponents(),
		Config:    session.SessionConfig{MaxLoops: r.limits.PlanNodeMaxLoops},
		Telemetry: r.hook,
		ModelName: r.model,
	}
}

// nodeSessionID 派生节点会话 ID（判据在 node 域：以系统提示为种子的稳定 hash）。
func (r *Runtime) nodeSessionID(systemPrompt string) string {
	return seenode.NodeSessionID(systemPrompt)
}

// registerForkTool 注册 fork_subagents（RegisterBuiltins 内调用）。
//
// 这一批子代理只走作业化派发（与 bash_bg / read_batch 同一个作业面）：调用立刻
// 返回每个子代理的句柄，产出经 bg_manage(op=fetch, handle) 取回。没有"阻塞到全部
// 跑完"的分支——阻塞期间模型没有下一次调用，"观察/提前终止子代理"就无从谈起。
func (r *Runtime) registerForkTool() {
	r.RegisterTool("fork_subagents",
		"Fork N isolated subagents in parallel (worktree-isolated) as background jobs."+fork.SubagentsContractDescription,
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subagents": map[string]interface{}{
					"type":        "array",
					"description": "Subagent specs: unique id + natural-language goal, plus optional per-subagent execution params.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							// 必填且非空：JSON Schema 的 required 只保证键在，空串照样能过；
							// 而空 id 会在 handler 里被拒。
							"id": map[string]interface{}{
								"type": "string", "minLength": 1,
								"description": "这条子代理的稳定标识（同一批内唯一、非空）：产出正文与工作表格行都按它认人。",
							},
							"goal": map[string]interface{}{
								"type": "string", "minLength": 1,
								"description": "自然语言的达成目标（非空）：写清要它交什么、验收判据是什么。",
							},
							"max_loops": map[string]interface{}{
								"type": "integer", "minimum": 1,
								"description": "这条子代理自己的 ReAct 循环上限（可选）。缺省 = 本批默认（配置 limits.plan_node_max_loops，默认 384）。与 effort 档位无关：切档位不会改变子代理的次数。只有越过进程级循环天花板（limits.input_loop_limit，默认 9999）才在调用处直接拒绝，不静默截断。",
							},
						},
						"required": []string{"id", "goal"},
					},
				},
				"max_concurrency": map[string]interface{}{"type": "integer", "minimum": 1},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"description": "本批 fork 总超时（秒）。长任务可省略（默认 limits.fork_timeout，2h）；简单审查/只读任务建议给 1200（20 分钟）等更紧上限，避免排队或异常时挂太久。",
				},
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
