package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	return r.runPlan(ctx, loaded)
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
			Input: SeelexNodeInput{ID: spec.ID, Input: spec.Goal, Kind: "agent"},
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

// forkSummaryLineLimit 是单子代理摘要行上限（保持工具结果紧凑）。
const forkSummaryLineLimit = 160

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
		b.WriteString(": ")
		b.WriteString(forkResultSummaryLine(wc.PrevResults[id]))
		b.WriteByte('\n')
	}
	b.WriteString("（完整会话/上下文/工具活动见工作区子代理树，点击节点查看详情）")
	return b.String(), nil
}

// forkResultSummaryLine 提取子代理输出的单行摘要：内核把结果编码为 JSON
// （RawString 带引号/转义）→ 先解码回纯文本，再取首个非空行截断到
// forkSummaryLineLimit；无输出 → "(无输出)"。
func forkResultSummaryLine(output string) string {
	if decoded := ""; json.Unmarshal([]byte(output), &decoded) == nil {
		output = decoded
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "(无输出)"
	}
	line := output
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "(无输出)"
	}
	if len(line) > forkSummaryLineLimit {
		return line[:forkSummaryLineLimit] + "…"
	}
	return line
}
