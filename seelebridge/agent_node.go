package seelebridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"

	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// SeelexAgentNode 是 plan kind:agent 节点的子代理执行包装（plan.md §3.3.1）。
//
// 它包装 bridge.NewAgentFactory 产物（factory func() node.AgentFactory）：
// Run 时先把节点作用域（NodeScope）与节点级 PromptBlocks（目标 + 父证据 +
// 预算）注入 ctx，再委托节点自己的 Session 执行。可见性策略 / 账号选择器 /
// 节点装配器都从 ctx 读取作用域与块，因此并行的子代理各得其域，不需要
// 框架侧的分支运行时。
type SeelexAgentNode struct {
	node.BaseNode
	input   SeelexNodeInput
	factory func() node.AgentFactory      // bridge.NewAgentFactory 产物
	scope   func() NodeScope              // 惰性解析：plan_run 时 binding 已冻结
	blocks  func() []seelectx.PromptBlock // 节点级 PromptBlocks（目标/证据/预算）
}

// newSeelexAgentNode 构造 agent 节点包装。scope/blocks 经闭包延迟解析，
// 使 plan_load（buildNode）到 plan_run 之间冻结的 PlanBranchBinding
// 与父证据都能被观察到。
func newSeelexAgentNode(spec codec.NodeSpec[SeelexNodeInput], runtime *Runtime) *SeelexAgentNode {
	return &SeelexAgentNode{
		BaseNode: node.NewBaseNode(spec.ID, node.KindAgent),
		input:    spec.Input,
		factory:  runtime.currentAgentFactory,
		scope:    func() NodeScope { return nodeScopeFor(runtime, spec.ID) },
		blocks:   func() []seelectx.PromptBlock { return runtime.nodePromptBlocks(spec.Input) },
	}
}

// Run 注入节点作用域与节点级 PromptBlocks 后委托节点 Session 执行
// （目标作为会话 system prompt，节点输入作为本轮请求）。
func (n *SeelexAgentNode) Run(ctx context.Context, _ *workplanTypes.WorkflowContext) (string, error) {
	// 1) 节点身份：可见性策略 / 账号选择器 / 装配器从 ctx 读取。
	ctx = WithNodeScope(ctx, n.scope())
	// 2) 节点上下文（PromptBlocks）：目标 + 父证据 + 预算。
	if n.blocks != nil {
		ctx = withNodePromptBlocks(ctx, n.blocks())
	}
	factory := n.factory()
	if factory == nil {
		return "", fmt.Errorf("agent node %q: agent factory is not configured", n.ID())
	}
	agent := factory.NewAgent(n.input.Input)
	return agent.Chat(ctx, n.input.Input)
}

// nodeScopeFor 解析节点作用域：新执行模型下分支即节点（BranchID = NodeID），
// 角色按 binding 与 entry 判定（与分支账号路由语义一致）。
func nodeScopeFor(runtime *Runtime, nodeID string) NodeScope {
	binding := runtime.currentPlanBranchBinding()
	return NodeScope{
		NodeID:      nodeID,
		Role:        roleForPlanBranch(binding, nodeID),
		BranchID:    nodeID,
		WorkspaceID: binding.WorkspaceID,
	}
}

// ── 节点级 PromptBlocks ─────────────────────────────────────────────

// nodeScopeAssembler 是节点子代理会话的 RequestAssembler：把 SeelexAgentNode
// 注入 ctx 的节点级 PromptBlocks 合并进每次请求（保序：静态块在前，节点块在后，
// 再跟 working history），委托默认装配器完成拼装。
type nodeScopeAssembler struct{}

func (nodeScopeAssembler) Assemble(ctx context.Context, request seelectx.AssemblyRequest) (seelectx.AssembledRequest, error) {
	if blocks := nodePromptBlocksFromContext(ctx); len(blocks) > 0 {
		merged := make([]seelectx.PromptBlock, 0, len(request.Blocks)+len(blocks))
		merged = append(merged, request.Blocks...)
		merged = append(merged, blocks...)
		request.Blocks = merged
	}
	return seelectx.DefaultRequestAssembler{}.Assemble(ctx, request)
}

// nodePromptBlocks 构建节点级 PromptBlock：目标 + 父证据 + 预算。
// 父证据经 SetNodeParentEvidence 注入（seelexctx snapshot 承袭，缺省无）。
func (r *Runtime) nodePromptBlocks(input SeelexNodeInput) []seelectx.PromptBlock {
	blocks := make([]seelectx.PromptBlock, 0, 3)
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-goal",
		Messages: []types.Message{{
			Role:    "user",
			Content: stringPtr("## 节点目标 (Node Goal)\n" + input.Input),
		}},
	})
	if evidence := r.nodeParentEvidence(); evidence != nil {
		blocks = append(blocks, seelectx.PromptBlock{
			Name: "parent-evidence",
			Messages: []types.Message{{
				Role:    "user",
				Content: stringPtr(evidence.Format()),
			}},
		})
	}
	budget := r.nodeBudget()
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-budget",
		Messages: []types.Message{{
			Role: "user",
			Content: stringPtr(fmt.Sprintf(
				"## 节点预算 (Node Budget)\n- 最大迭代轮数: %d\n- 最大输出 tokens: %d",
				budget.MaxLoops, budget.MaxOutputTokens)),
		}},
	})
	return blocks
}

// nodeBudgetInfo 是节点子代理的执行预算（渲染为 PromptBlock，并作为
// 节点 Session 的 SessionConfig 生效）。
type nodeBudgetInfo struct {
	MaxLoops        int
	MaxOutputTokens int
}

// defaultNodeMaxLoops 是节点子代理的默认迭代轮数预算（比主会话的 25 更
// 收敛，节点只做片段工作）。
const defaultNodeMaxLoops = 15

func (r *Runtime) nodeBudget() nodeBudgetInfo {
	limits := r.currentAccountLimits()
	return nodeBudgetInfo{MaxLoops: defaultNodeMaxLoops, MaxOutputTokens: limits.MaxOutputTokens}
}

// parentEvidenceMu 保护父证据提供者（并发 plan_run 时读）。
type parentEvidenceState struct {
	mu       sync.RWMutex
	provider func() *snapshot.ContextSnapshot
}

// nodeParentEvidence 返回当前父级上下文快照（nil 表示不注入证据块）。
func (r *Runtime) nodeParentEvidence() *snapshot.ContextSnapshot {
	if r == nil || r.parentEvidence == nil {
		return nil
	}
	r.parentEvidence.mu.RLock()
	provider := r.parentEvidence.provider
	r.parentEvidence.mu.RUnlock()
	if provider == nil {
		return nil
	}
	return provider()
}

// SetNodeParentEvidence 注入父级上下文快照提供者：节点执行时把快照格式化为
// 父证据 PromptBlock（seelexctx snapshot 承袭）。传入 nil 关闭证据注入。
func (r *Runtime) SetNodeParentEvidence(provider func() *snapshot.ContextSnapshot) {
	if r == nil {
		return
	}
	r.parentEvidence.mu.Lock()
	r.parentEvidence.provider = provider
	r.parentEvidence.mu.Unlock()
}

func stringPtr(value string) *string { return &value }
