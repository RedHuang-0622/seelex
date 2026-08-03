package seelebridge

import (
	"context"
	"fmt"

	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/merger"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
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
	runtime *Runtime                      // merge-back 回传面（父会话/父证据/遥测）
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
		runtime:  runtime,
	}
}

// parentSnapshot 返回父证据快照（ContextExchanger.ParentEvidence 消息进）。
func (n *SeelexAgentNode) parentSnapshot() *snapshot.ContextSnapshot {
	if n == nil || n.runtime == nil {
		return nil
	}
	return n.runtime.nodeParentEvidence()
}

// traceSource 返回遥测追踪源（子代理 Findings/Decisions 提取）。
func (n *SeelexAgentNode) traceSource() provider.TraceSource {
	if n == nil || n.runtime == nil {
		return nil
	}
	return n.runtime.Tracer()
}

// now 是时间戳提供者（测试可覆盖；Date.now 语义保持确定）。
var now = time.Now

// Run 注入节点作用域与节点级 PromptBlocks 后委托节点 Session 执行
// （目标作为会话 system prompt，节点输入作为本轮请求）。
// 执行成功后把子代理会话的结构化上下文合并回父会话（merge-back）。
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
	result, err := agent.Chat(ctx, n.input.Input)
	if err == nil {
		n.mergeBack(ctx, agent, n.input.Input)
	}
	return result, err
}

// mergeBack 把子代理会话的结构化上下文（Findings/Decisions/Constraints/
// TokenEstimate）合并回父会话：子快照 + 父快照 → merger.MergeBack →
// Format() 文本经 sink 回传（application 侧排队，下一次 ChatStream 开始前
// 注入父会话）。
//
// 重要：不得直接调用父会话的 AppendHistory/History——plan_run 作为主代理的
// 工具调用在 Session.ChatStream 内同步执行，主会话锁被全程持有，任何子代理
// goroutine 对主会话的访问都会死锁（冒烟测试实测 19 分钟死锁）。回传必须
// 走 ContextExchanger.MergeBack 消息出（mailbox 投递）。
func (n *SeelexAgentNode) mergeBack(ctx context.Context, agent node.Agent, goal string) {
	childSession, ok := agent.(*frameworkSession.Session)
	if !ok {
		return
	}
	child := seelexctx.ExportSnapshot(childSession, n.traceSource(), goal)
	parent := n.parentSnapshot()
	if parent == nil {
		parent = &snapshot.ContextSnapshot{SourceSessionID: childSession.SessionID(), ExportedAt: now()}
	}
	if err := merger.NewMerger().MergeBack(parent, child); err != nil {
		return
	}
	content := parent.Format()
	if sink := n.mergeBackSink(); sink != nil {
		sink(content)
	}
}

// mergeBackSink 返回子代理上下文回传接收器（Actor 消息出：投递到主 actor
// 的 mailbox；经 ContextExchanger 接口，nil = 未接线，回传跳过——绝不
// 直接访问父会话，避免锁死锁）。
func (n *SeelexAgentNode) mergeBackSink() func(string) {
	if n == nil || n.runtime == nil {
		return nil
	}
	exchanger := n.runtime.contextExchanger()
	if exchanger == nil {
		return nil
	}
	return exchanger.MergeBack
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
// 父证据经 ContextExchanger.ParentEvidence 注入（actor.go，缺省无）。
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

// nodeBudget 返回节点子代理的迭代轮数预算：优先 seele.yaml limits 的
// plan_node_max_loops（调参入口），默认 15（比主会话更收敛，节点只做片段工作）。
func (r *Runtime) nodeBudget() nodeBudgetInfo {
	limits := r.currentAccountLimits()
	return nodeBudgetInfo{MaxLoops: r.limits.PlanNodeMaxLoops, MaxOutputTokens: limits.MaxOutputTokens}
}

// nodeParentEvidence 返回当前父级上下文快照（Actor 消息进：主 actor 对外
// 投影；nil = 未装配交换器，不注入证据块）。经 ContextExchanger 接口读取，
// 实现不得访问 ChatStream 中的主会话（死锁教训见 actor.go）。
func (r *Runtime) nodeParentEvidence() *snapshot.ContextSnapshot {
	exchanger := r.contextExchanger()
	if exchanger == nil {
		return nil
	}
	return exchanger.ParentEvidence()
}

func stringPtr(value string) *string { return &value }
