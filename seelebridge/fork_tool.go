package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/codec"
)

// ── fork_subagents 工具（docs/2026-08-03-subagent-fork-architecture/plan.md §4）──
// 模型自由层的轻量委派入口：程序化构造 all-parallel DAG（start → N×agent
// 节点 → summary），复用 workplan runner / 事件投影 / NodeScope / worktree
// 生命周期 / merge-back 全链路。模型不需要 DAG 知识——只传子代理 id + goal。
//
// 与 plan_load/plan_run 的关系：fork 是无依赖 DAG 的编程生成特例，两者共用
// 同一执行内核；fork 不要求模型产出规范 JSON（弱模型可用）。


// registerForkTool 注册 fork_subagents（RegisterBuiltins 内调用）。
func (r *Runtime) registerForkTool() {
	r.RegisterTool("fork_subagents",
		"Fork N isolated subagents in parallel (worktree-isolated) and return their structured outputs."+forkSubagentsContractDescription,
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

// forkSubagentsHandler 是 fork_subagents 的执行入口。
func (r *Runtime) forkSubagentsHandler(ctx context.Context, argsJSON string) (string, error) {
	var input forkSubagentsInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("fork_subagents: invalid args: %w", err)
	}
	if len(input.Subagents) == 0 {
		return "", fmt.Errorf("fork_subagents: at least one subagent is required")
	}
	seen := make(map[string]bool, len(input.Subagents))
	for _, spec := range input.Subagents {
		if spec.ID == "" || strings.TrimSpace(spec.Goal) == "" {
			return "", fmt.Errorf("fork_subagents: subagent %q must have id and goal", spec.ID)
		}
		if seen[spec.ID] {
			return "", fmt.Errorf("fork_subagents: duplicate subagent id %q", spec.ID)
		}
		seen[spec.ID] = true
	}
	// 护栏：数量上限（PlanPolicy.MaxNodes；未配置不限）。
	if policy := r.currentPlanPolicy(); policy.MaxNodes > 0 && len(input.Subagents) > policy.MaxNodes {
		return "", fmt.Errorf("fork_subagents: %d subagents exceeds policy limit %d", len(input.Subagents), policy.MaxNodes)
	}

	// B6 装配件：fork 派工前做 task 幂等校验——按归一化 goal 查注册表，
	// 命中 → 绑既有 task_id；未命中 → 子代理自己开一个 task。只绑 task_id，
	// 不注入 task 内容（保持子代理 prompt 格式纯净）。
	taskBindings := make(map[string]string, len(input.Subagents))
	for _, spec := range input.Subagents {
		if taskID := r.bindSubagentTask(spec); taskID != "" {
			taskBindings[spec.ID] = taskID
		}
	}

	// 结果复用（省 token）：若所有子代理都命中「既有已完成 task + 子代理树
	// 保留完整输出」（典型场景：结果返回失败——final_output 被截断或
	// read_tool_result 失败——需要 retry），直接读回已保存输出并返回，
	// 不再重新执行。只有全部命中才短路；部分命中仍整体重跑，避免 DAG
	// 出现混合状态（保守策略，README 注明）。
	if summaries, ok := r.reusableForkSummaries(input.Subagents); ok {
		for _, spec := range input.Subagents {
			if taskID := taskBindings[spec.ID]; taskID != "" {
				// bindSubagentTask 已把终态 task 置 retry（RetryCount 自增）；
				// 复用成功 → 置回 completed，计数保留（worktable 显示
				// DONE，retry_count 保留，未读签名变化）。
				_, _ = r.TaskSetStatus(taskID, TaskCompleted, "fork reused stored output")
			}
		}
		return r.forkReuseResultJSON(input.Subagents, summaries)
	}

	loaded, err := r.buildForkPlan(input, taskBindings)
	if err != nil {
		return "", err
	}
	// 子代理树（内存态，不落盘）：记录 parent/child 链——父节点是发起 fork
	// 的子代理（NodeScope 携带节点 ID；嵌套 fork）或主代理（main 合成根）。
	parentID := mainAgentNodeID
	if scope, ok := NodeScopeFromContext(ctx); ok && scope.NodeID != "" && scope.Role == RoleSubAgent {
		parentID = scope.NodeID
	}
	r.subagentTree.registerFork(parentID, input.Subagents)
	// fork 宽松超时（2026-08-08）：fork 是同步编排工具，总时长 = 全部子代理
	// 工作量之和，通用工具超时（tool_call_timeout 默认 30 分钟）会掐死长任务。
	// 剥离外层截止时间（保留用户取消传播），改用 limits.fork_timeout（默认 2h）。
	forkTimeout := time.Duration(r.limits.ForkTimeoutSec) * time.Second
	if forkTimeout <= 0 {
		forkTimeout = 2 * time.Hour
	}
	forkCtx, forkCancel := context.WithTimeout(context.Background(), forkTimeout)
	stop := context.AfterFunc(ctx, forkCancel) // 原 ctx 取消（用户停止）→ 同步取消 fork
	defer stop()
	defer forkCancel()
	return r.planExecutor.RunPlan(forkCtx, loaded, false)
}

// reusableForkSummaries 检查每个 spec 是否可复用已保存输出：goal 命中的
// 既有 task 已完成，且子代理树仍保留该节点的完整输出。全部命中才返回
// (摘要表, true)；任一缺失返回 (nil, false)。
func (r *Runtime) reusableForkSummaries(specs []forkSubagentSpec) (map[string]string, bool) {
	if r == nil || len(specs) == 0 {
		return nil, false
	}
	summaries := make(map[string]string, len(specs))
	for _, spec := range specs {
		if _, found, _ := r.ResolveTaskByKey(taskKeyForGoal(spec.Goal)); !found {
			return nil, false
		}
		if summary := r.subagentTree.summaryFor(spec.ID); summary == "" {
			return nil, false
		} else {
			summaries[spec.ID] = summary
		}
	}
	return summaries, true
}

// forkReuseResultJSON 构造复用结果的 JSON（与 planRunResultJSON 外形一致：
// status/node_count/final_output；标记 reused=true 供审计，不计入重跑）。
func (r *Runtime) forkReuseResultJSON(specs []forkSubagentSpec, summaries map[string]string) (string, error) {
	var builder strings.Builder
	builder.WriteString("子代理完成情况（复用上次已保存输出，未重新执行）:\n")
	for _, spec := range specs {
		builder.WriteString("- ")
		builder.WriteString(spec.ID)
		builder.WriteString(": ")
		summary := strings.TrimSpace(summaries[spec.ID])
		if summary == "" {
			builder.WriteString("(无输出)\n")
			continue
		}
		builder.WriteString(strings.ReplaceAll(summary, "\n", "\n  "))
		builder.WriteByte('\n')
	}
	builder.WriteString("（输出来自子代理树已保存证据；如需更早版本请清理子代理树后重跑）")
	payload := map[string]any{
		"status":       "completed",
		"node_count":   len(specs),
		"final_output": builder.String(),
		"reused":       true,
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

// buildForkPlan 程序化构造 fork DAG：
//
//	start(auto) ──→ s1(agent) ──┐
//	   └──────────→ s2(agent) ──┤──→ summary(summary, 拼接全部输出)
//	   └──────────→ sN(agent) ──┘
//
// summary 节点读取 WorkflowContext.PrevResults 拼接各子代理输出，
// 作为 fork 的最终输出（FinalOutput）。
// bindSubagentTask 解析/创建子代理 task 并返回 task_id（幂等：相同 goal
// 命中同一 task；新开时以 subagent:<id> 作为 ID，随后参与者合并）。
func (r *Runtime) bindSubagentTask(spec forkSubagentSpec) string {
	key := taskKeyForGoal(spec.Goal)
	if existing, found, _ := r.ResolveTaskByKey(key); found {
		_, _ = r.TaskAttachParticipant(existing.ID, spec.ID)
		// 既有 task 被该子代理重新接手：
		//   - 终态（completed/failed）→ 重试语义：置 retry（RetryCount
		//     自增，worktable 显示 RETRY n），节点真正启动时再转 running；
		//   - 已在 retry/running/doing → 保持现状（不降级）；
		//   - 其余（pending/queued）→ 排队（B5 生命周期打点）。
		switch existing.Status {
		case TaskCompleted, TaskFailed:
			_, _ = r.TaskSetStatus(existing.ID, TaskRetry, "fork retried")
		case TaskRetry, TaskRunning, TaskDoing:
			// 保持当前状态（retry 保留计数；running 不允许回退）。
		default:
			_, _ = r.TaskSetStatus(existing.ID, TaskQueued, "fork scheduled")
		}
		return existing.ID
	}
	created, _, err := r.TaskAdd(TaskSpec{
		ID: "subagent:" + spec.ID, Key: key, Phase: TaskPhaseSubagent, Task: spec.Goal,
		Kind: "subagent", Assignee: spec.ID, SourceID: spec.ID,
	})
	if err != nil {
		return ""
	}
	_, _ = r.TaskAttachParticipant(created.ID, spec.ID)
	_, _ = r.TaskSetStatus(created.ID, TaskQueued, "fork scheduled")
	return created.ID
}

func (r *Runtime) buildForkPlan(input forkSubagentsInput, taskBindings map[string]string) (*loadedPlanDoc, error) {
	nodeCount := len(input.Subagents) + 2 // start + subagents + summary
	policy := r.currentPlanPolicy()
	maxFork := input.MaxConcurrency
	if maxFork <= 0 {
		maxFork = policy.Concurrency(nodeCount)
	}
	document := codec.Document[SeelexNodeInput]{
		Version: codec.Version,
		Entry:   "start",
		Nodes: []codec.NodeSpec[SeelexNodeInput]{
			{ID: "start", Kind: "auto", Input: SeelexNodeInput{ID: "start", Input: "fork start", Kind: "auto"}},
		},
	}
	for _, spec := range input.Subagents {
		document.Nodes = append(document.Nodes, codec.NodeSpec[SeelexNodeInput]{
			ID: spec.ID, Kind: "agent",
			Input: SeelexNodeInput{
				ID: spec.ID, Input: spec.Goal, Kind: "agent",
				// fork 子代理节点循环预算复用 effort 调节的节点循环数
				// （PlanPolicy.MaxNodeLoops：high=48 / max=96；未设置 → 回退
				// 通用 PlanNodeMaxLoops）——子代理循环数与主代理/plan 节点
				// 同一套 effort 语义，不做独立常量。
				Budget: &NodeBudgetInput{MaxLoops: forkNodeLoops(r)},
				// B6：装配现成 task_id（无内容，不污染 prompt）。
				TaskID: taskBindings[spec.ID],
			},
		})
	}
	document.Nodes = append(document.Nodes, codec.NodeSpec[SeelexNodeInput]{
		ID: "summary", Kind: "summary",
		Input: SeelexNodeInput{ID: "summary", Input: "summarize all subagent outputs", Kind: "summary"},
	})
	// 边：start → 每个子代理；每个子代理 → summary。
	document.Edges = append(document.Edges, codec.EdgeSpec{From: "start", To: "summary"})
	for _, spec := range input.Subagents {
		document.Edges = append(document.Edges,
			codec.EdgeSpec{From: "start", To: spec.ID},
			codec.EdgeSpec{From: spec.ID, To: "summary"},
		)
	}
	plan, err := codec.Render(document, r.nodeFactory())
	if err != nil {
		return nil, fmt.Errorf("fork_subagents: build DAG: %w", err)
	}
	return &loadedPlanDoc{
		Canonical:   forkPlanCanonical(input),
		Entry:       "start",
		NodeCount:   nodeCount,
		EdgeCount:   len(document.Edges),
		MaxForkConc: maxFork,
		Plan:        plan,
	}, nil
}






// forkNodeLoops 返回 fork 子代理节点的循环预算：复用 effort 调节的节点
// 循环数（currentPlanPolicy().MaxNodeLoops，high=48 / max=96）；未设置
// （lite/medium 或未切换 effort）→ 回退通用 PlanNodeMaxLoops。
func forkNodeLoops(r *Runtime) int {
	if r == nil {
		return 0
	}
	if policy := r.currentPlanPolicy(); policy.MaxNodeLoops > 0 {
		return policy.MaxNodeLoops
	}
	return r.limits.PlanNodeMaxLoops
}




