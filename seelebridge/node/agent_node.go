// Package node 承载 plan kind:agent 节点的子代理执行域：节点包装、节点级
// PromptBlocks、执行预算与 skill 匹配。运行时能力经 Deps 由根包注入，
// 域内不依赖 seelebridge 根包（避免循环依赖）。
package node

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	frameworknode "github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/skill"
)

// Deps 是 AgentNode 执行所需的运行时回调集合，由根包（Runtime）注入。
type Deps struct {
	CurrentAgentFactory      func() frameworknode.AgentFactory
	CurrentPlanBranchBinding func() plan.PlanBranchBinding
	AppendNodePhase          func(ctx context.Context, nodeID, status string)
	BeginNodeWorktree        func(scope model.NodeScope, nodeID string) *worktree.NodeWorktree
	FinishNodeWorktree       func(ctx context.Context, nodeID string, wt *worktree.NodeWorktree) error
	ReleaseNodeWorktree      func(nodeID string)
	RegisterNodeSession      func(nodeID string, sess *frameworkSession.Session, goal string)
	UnregisterNodeSession    func(nodeID string)
	CompleteSubagentNode     func(nodeID, summary string, err error)
	NodeParentEvidence       func() *snapshot.ContextSnapshot
	MergeBackIntoParent      func(child *snapshot.ContextSnapshot) *snapshot.ContextSnapshot
	EnqueueSubagentContext   func(content string)
	NodeBudget               func(input plan.SeelexNodeInput) NodeBudgetInfo
	NodePromptBlocks         func(input plan.SeelexNodeInput) []seelectx.PromptBlock
	Tracer                   func() provider.TraceSource
}

// NodeBudgetInfo 是节点子代理的执行预算（渲染进 PromptBlock，并作为
// 节点 Session 的 SessionConfig 生效）。
type NodeBudgetInfo struct {
	MaxLoops        int
	MaxOutputTokens int
}

// AgentNode 是 plan kind:agent 节点的子代理执行包装（plan.md §3.3.1）。
// Run 时先注入节点作用域（NodeScope）与节点级 PromptBlocks，再委托节点
// 自己的 Session 执行。
type AgentNode struct {
	frameworknode.BaseNode
	input   plan.SeelexNodeInput
	factory func() frameworknode.AgentFactory
	scope   func() model.NodeScope // 惰性解析：plan_run 时 binding 已冻结
	blocks  func() []seelectx.PromptBlock
	deps    Deps
}

// NewAgentNode 构造 agent 节点包装。scope/blocks 经闭包延迟解析，使
// plan_load（buildNode）到 plan_run 之间冻结的 PlanBranchBinding 与
// 父证据都能被观察到。
func NewAgentNode(spec codec.NodeSpec[plan.SeelexNodeInput], deps Deps) *AgentNode {
	taskID := spec.Input.TaskID
	return &AgentNode{
		BaseNode: frameworknode.NewBaseNode(spec.ID, frameworknode.KindAgent),
		input:    spec.Input,
		factory:  deps.CurrentAgentFactory,
		scope: func() model.NodeScope {
			scope := NodeScopeFor(deps, spec.ID)
			scope.TaskID = taskID
			return scope
		},
		blocks: func() []seelectx.PromptBlock { return deps.NodePromptBlocks(spec.Input) },
		deps:   deps,
	}
}

// Input 返回节点输入（测试/诊断读取面）。
func (n *AgentNode) Input() plan.SeelexNodeInput { return n.input }

// Scope 返回节点作用域解析器（测试/诊断读取面）。
func (n *AgentNode) Scope() func() model.NodeScope { return n.scope }

// Blocks 返回节点 PromptBlocks 解析器（测试/诊断读取面）。
func (n *AgentNode) Blocks() func() []seelectx.PromptBlock { return n.blocks }

// traceSource 返回遥测追踪源（子代理 Findings/Decisions 提取）。
func (n *AgentNode) traceSource() provider.TraceSource {
	if n == nil || n.deps.Tracer == nil {
		return nil
	}
	return n.deps.Tracer()
}

// now 是时间戳提供者（测试可覆盖；Date.now 语义保持确定）。
var now = time.Now

// Run 注入节点作用域与节点级 PromptBlocks 后委托节点 Session 执行。
// worktree 生命周期（plan.md §3）：RoleSubAgent 节点先开 worktree 并把
// NodeScope.WorkspaceID 指向它，执行结束后变基仓库 + 合并审批 + merge + 清理。
func (n *AgentNode) Run(ctx context.Context, _ *workplanTypes.WorkflowContext) (string, error) {
	scope := n.scope()
	if scope.Role == model.RoleSubAgent {
		n.deps.AppendNodePhase(ctx, n.ID(), "worktree_creating")
	}
	wt := n.deps.BeginNodeWorktree(scope, n.ID())
	if wt != nil {
		scope.WorkspaceID = wt.Path
	}
	ctx = model.WithNodeScope(ctx, scope)
	if scope.Role == model.RoleSubAgent {
		n.deps.AppendNodePhase(ctx, n.ID(), "running")
	}
	if n.blocks != nil {
		ctx = WithNodePromptBlocks(ctx, n.blocks())
	}
	factory := n.factory()
	if factory == nil {
		return "", fmt.Errorf("agent node %q: agent factory is not configured", n.ID())
	}
	agent := factory.NewAgent(n.input.Input)
	if sess, ok := agent.(*frameworkSession.Session); ok {
		sess.SetMaxLoops(n.deps.NodeBudget(n.input).MaxLoops)
		n.deps.RegisterNodeSession(n.ID(), sess, n.input.Input)
		defer n.deps.UnregisterNodeSession(n.ID())
	}
	result, err := agent.Chat(ctx, n.input.Input)
	n.deps.CompleteSubagentNode(n.ID(), result, err)
	// merge-back 不因 Chat 失败/超时而跳过：子代理在超时前可能已积累
	// Findings/Decisions（长时间静置场景），整块丢弃会造成"子代理跑完
	// 但父侧无产出"。合并本身幂等——失败时快照可能为空，merger 只保留
	// 父证据，无害（2026-08-10 C 修复）。
	n.mergeBack(ctx, agent, n.input.Input)
	if wt != nil {
		if err == nil {
			err = n.deps.FinishNodeWorktree(ctx, n.ID(), wt)
		}
		if err == nil {
			n.deps.ReleaseNodeWorktree(n.ID())
		}
	}
	return result, err
}

// mergeBack 把子代理会话的结构化上下文（Findings/Decisions/Constraints/
// TokenEstimate）合并回父会话：子快照 + 父快照 → merger.MergeBack →
// 合并结果写回 parentEvidence（后续子代理/嵌套 fork 可累积看到）→
// Format() 文本经 sink 回传（application 侧排队，下一次 ChatStream 开始
// 前注入父会话）。
func (n *AgentNode) mergeBack(ctx context.Context, agent frameworknode.Agent, goal string) {
	childSession, ok := agent.(*frameworkSession.Session)
	if !ok {
		return
	}
	child := seelexctx.ExportSnapshot(childSession, n.traceSource(), goal)
	merged := n.deps.MergeBackIntoParent(child)
	if merged == nil {
		return
	}
	content := merged.Format()
	if sink := n.deps.EnqueueSubagentContext; sink != nil {
		sink(content)
	}
}

// NodeScopeFor 解析节点作用域：新执行模型下分支即节点（BranchID = NodeID），
// 角色按 binding 与 entry 判定（与分支账号路由语义一致）。
func NodeScopeFor(deps Deps, nodeID string) model.NodeScope {
	binding := deps.CurrentPlanBranchBinding()
	return model.NodeScope{
		NodeID:      nodeID,
		Role:        RoleForPlanBranch(binding, nodeID),
		BranchID:    nodeID,
		WorkspaceID: binding.WorkspaceID,
	}
}

// RoleForPlanBranch 判定分支角色：entry 节点用 PrimaryRole；"_" 前缀为
// goalplan；其余为 subagent。
func RoleForPlanBranch(binding plan.PlanBranchBinding, branchID string) model.AccountRole {
	if branchID == binding.EntryNodeID {
		return binding.PrimaryRole
	}
	if strings.HasPrefix(branchID, "_") {
		return model.RoleGoalPlan
	}
	return model.RoleSubAgent
}

type nodePromptBlocksContextKey struct{}

// WithNodePromptBlocks 把节点级 PromptBlocks（目标 + 父证据 + 预算）注入 ctx。
func WithNodePromptBlocks(ctx context.Context, blocks []seelectx.PromptBlock) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, nodePromptBlocksContextKey{}, append([]seelectx.PromptBlock(nil), blocks...))
}

// NodePromptBlocksFromContext 读取 ctx 中的节点级 PromptBlocks（拷贝）。
func NodePromptBlocksFromContext(ctx context.Context) []seelectx.PromptBlock {
	if ctx == nil {
		return nil
	}
	blocks, _ := ctx.Value(nodePromptBlocksContextKey{}).([]seelectx.PromptBlock)
	return append([]seelectx.PromptBlock(nil), blocks...)
}

// NodeSubagentCharter 渲染子代理章程（Claude Code 风格结构化提示词：
// Role/Context/Task/Investigation/Constraints/Verification）。提示词正文
// 收录在 internal/promptassets/assets/subagent/charter.md（不硬编码），
// Go 侧只提供运行时事实（goal/预算/节点 ID/父证据）。
func NodeSubagentCharter(input plan.SeelexNodeInput, budget NodeBudgetInfo, evidence *snapshot.ContextSnapshot) string {
	data := promptassets.SubagentData{
		Goal:            input.Input,
		NodeID:          input.ID,
		MaxLoops:        budget.MaxLoops,
		MaxOutputTokens: budget.MaxOutputTokens,
	}
	if evidence != nil {
		data.Evidence = evidence.Format()
	}
	return promptassets.SubagentCharter(data)
}

// MatchNodeSkills 按节点目标匹配 skill：skill 名称（含分词：code-impl →
// code/impl）或描述中的英文词出现在目标文本中即匹配。
func MatchNodeSkills(input string, skills []skill.Skill) []skill.Skill {
	haystack := strings.ToLower(input)
	matched := make([]skill.Skill, 0, len(skills))
	for _, s := range skills {
		needle := strings.ToLower(s.Name)
		if strings.Contains(haystack, needle) {
			matched = append(matched, s)
			continue
		}
		for _, part := range strings.FieldsFunc(needle, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if len(part) >= 3 && strings.Contains(haystack, part) {
				matched = append(matched, s)
				goto next
			}
		}
		for _, word := range strings.Fields(s.Description) {
			w := strings.Trim(strings.ToLower(word), "，。！？.!?:;()（）\"'")
			if len(w) >= 3 && strings.Contains(haystack, w) {
				matched = append(matched, s)
				break
			}
		}
	next:
	}
	return matched
}
