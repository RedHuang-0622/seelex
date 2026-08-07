package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
)

// ── fork_subagents 工具（docs/2026-08-03-subagent-fork-architecture/plan.md §4）──
// 模型自由层的轻量委派入口：程序化构造 all-parallel DAG（start → N×agent
// 节点 → summary），复用 workplan runner / 事件投影 / NodeScope / worktree
// 生命周期 / merge-back 全链路。模型不需要 DAG 知识——只传子代理 id + goal。
//
// 与 plan_load/plan_run 的关系：fork 是无依赖 DAG 的编程生成特例，两者共用
// 同一执行内核；fork 不要求模型产出规范 JSON（弱模型可用）。

const forkSubagentsContractDescription = `
When to use fork_subagents:
- A task has multiple independent pieces of work that can run in parallel.
- You want delegated subagents to investigate/implement separate concerns
  and report structured findings back.
- Use instead of doing everything serially yourself when the work is isolated.

When not to use fork_subagents:
- Work with dependencies between steps (use a Plan instead, or do it yourself).
- A single simple task you can finish directly.
- Forking more than a handful of subagents (each subagent is a full agent
  session; keep the fork small and focused).

Contract:
- subagents: array of {id, goal}. Each id must be unique; each goal is a
  natural-language task for one isolated subagent (worktree-isolated when the
  project is a git repository).
- max_concurrency: optional cap on parallel subagents (default: policy limit).
- Returns a summary JSON with each subagent's output.
`

// forkSubagentsInput 是 fork_subagents 的参数契约。
type forkSubagentsInput struct {
	Subagents      []forkSubagentSpec `json:"subagents"`
	MaxConcurrency int                `json:"max_concurrency,omitempty"`
}

type forkSubagentSpec struct {
	ID   string `json:"id"`
	Goal string `json:"goal"`
}

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

	loaded, err := r.buildForkPlan(input)
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
	return r.runPlan(forkCtx, loaded)
}

// buildForkPlan 程序化构造 fork DAG：
//
//	start(auto) ──→ s1(agent) ──┐
//	   └──────────→ s2(agent) ──┤──→ summary(summary, 拼接全部输出)
//	   └──────────→ sN(agent) ──┘
//
// summary 节点读取 WorkflowContext.PrevResults 拼接各子代理输出，
// 作为 fork 的最终输出（FinalOutput）。
func (r *Runtime) buildForkPlan(input forkSubagentsInput) (*loadedPlanDoc, error) {
	nodeCount := len(input.Subagents) + 2 // start + subagents + summary
	policy := r.currentPlanPolicy()
	maxFork := input.MaxConcurrency
	if maxFork <= 0 {
		maxFork = policy.concurrency(nodeCount)
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
				// fork 子代理节点循环预算用独立宽松默认（limits.fork_node_max_loops，
				// 默认 60）：一个子代理常要串行处理多个实例/多步调研，15 轮默认
				// 预算（PlanNodeMaxLoops）对长任务不够。
				Budget: &NodeBudgetInput{MaxLoops: forkNodeLoops(r)},
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

// forkPlanCanonical 生成 fork DAG 的规范 JSON（审计/展示；非模型输入）。
func forkPlanCanonical(input forkSubagentsInput) string {
	encoded, _ := json.Marshal(input)
	return string(encoded)
}

// ── summary 节点 ──────────────────────────────────────────────────────

// forkSummaryNode 是 fork 的汇总节点：把全部前驱节点输出压缩为每子代理
// 一行的紧凑摘要（WorkflowContext.PrevResults，内核收集），作为 fork 最终
// 输出。完整输出不进入对话/历史（避免对话区被子代理大段内容灌满）——
// 子代理树（工作区）与节点详情弹窗承载完整会话/上下文/工具活动。
type forkSummaryNode struct {
	node.BaseNode
	input SeelexNodeInput
}

func newForkSummaryNode(spec codec.NodeSpec[SeelexNodeInput]) *forkSummaryNode {
	return &forkSummaryNode{
		BaseNode: node.NewBaseNode(spec.ID, node.KindMethod),
		input:    spec.Input,
	}
}

// forkSummaryLineLimit 是单行摘要长度上限；forkSummaryMaxLines 是每子代理
// 保留的行数（*N 而非 *1：子代理返回携带前 N 行，既信息充分又不灌满对话）。
// 每子代理总上限 = lineLimit × maxLines。长任务（多实例串行）的汇报常
// 一行一个实例，30 行可容纳一批 20+ 实例的逐条结论。
const (
	forkSummaryLineLimit = 160
	forkSummaryMaxLines  = 30
)

// forkNodeLoops 返回 fork 子代理节点的循环预算（limits.fork_node_max_loops；
// 0 → 默认 60）。
func forkNodeLoops(r *Runtime) int {
	if r != nil && r.limits.ForkNodeMaxLoops > 0 {
		return r.limits.ForkNodeMaxLoops
	}
	return 60
}

func (n *forkSummaryNode) Run(_ context.Context, wc *workplanTypes.WorkflowContext) (string, error) {
	if wc == nil || len(wc.PrevResults) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(wc.PrevResults))
	for id := range wc.PrevResults {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("子代理完成情况:\n")
	for _, id := range keys {
		b.WriteString("- ")
		b.WriteString(id)
		b.WriteByte(':')
		summary := forkResultSummaryLines(wc.PrevResults[id])
		if summary == "" {
			b.WriteString(" (无输出)\n")
			continue
		}
		b.WriteByte(' ')
		// 多行用 \n + 缩进续行展示（保持每行可读、整体有界）。
		b.WriteString(strings.ReplaceAll(summary, "\n", "\n  "))
		b.WriteByte('\n')
	}
	b.WriteString("（完整会话/上下文/工具活动见工作区子代理树，点击节点查看详情）")
	return b.String(), nil
}

// forkResultSummaryLines 提取子代理输出的有界摘要：内核把结果编码为 JSON
// （RawString 带引号/转义）→ 先解码回纯文本，再保留前 forkSummaryMaxLines
// 个非空行（每行截断到 forkSummaryLineLimit）；无输出 → 空串。
func forkResultSummaryLines(output string) string {
	if decoded := ""; json.Unmarshal([]byte(output), &decoded) == nil {
		output = decoded
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > forkSummaryLineLimit {
			line = line[:forkSummaryLineLimit] + "…"
		}
		lines = append(lines, line)
		if len(lines) >= forkSummaryMaxLines {
			break
		}
	}
	return strings.Join(lines, "\n")
}
