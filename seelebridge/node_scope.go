package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/seelectx"
)

// NodeScope 是 Plan 节点的执行作用域：子代理身份、角色与工作区信息
// （plan.md §3.3.1 / 架构文档 §4.4）。
//
// SeelexAgentNode.Run 把 NodeScope 注入请求 ctx；可见性策略（按角色过滤
// 工具）、账号选择器（按角色 + branchID 确定性 hash）与节点装配器
// （节点级 PromptBlocks）都从 ctx 读取它，因此同一 Runtime 上并行的多个
// 子代理互不串台。
type NodeScope struct {
	NodeID      string
	Role        AccountRole
	BranchID    string
	WorkspaceID string
	// TaskID 是装配件模式绑定的现成 task（B6）：只作为绑定元数据，不进
	// 任何 prompt 块——避免外部 task 内容污染子代理 prompt 格式与处理。
	TaskID string
}

type nodeScopeContextKey struct{}

// WithNodeScope 把节点作用域注入 ctx（子代理执行请求）。
func WithNodeScope(ctx context.Context, scope NodeScope) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, nodeScopeContextKey{}, scope)
}

// NodeScopeFromContext 读取 ctx 中的节点作用域；无作用域时返回 (零值, false)。
// 主代理（非子代理）请求不注入作用域，调用方按 false 走主链路逻辑。
func NodeScopeFromContext(ctx context.Context) (NodeScope, bool) {
	if ctx == nil {
		return NodeScope{}, false
	}
	scope, ok := ctx.Value(nodeScopeContextKey{}).(NodeScope)
	return scope, ok
}

// nodeScopeFromContextOrEmpty 读取节点作用域；无作用域时返回空 NodeScope
// （NodeID 为空，可见性策略据此放行主代理全量工具）。
func nodeScopeFromContextOrEmpty(ctx context.Context) NodeScope {
	if scope, ok := NodeScopeFromContext(ctx); ok {
		return scope
	}
	return NodeScope{}
}

type nodePromptBlocksContextKey struct{}

// withNodePromptBlocks 把节点级 PromptBlocks（目标 + 父证据 + 预算）注入 ctx。
// 节点会话的装配器（nodeScopeAssembler）在每次请求时读取并合并进请求，
// 因此同一节点会话的多次迭代都能携带这些块，且不污染其它节点。
func withNodePromptBlocks(ctx context.Context, blocks []seelectx.PromptBlock) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, nodePromptBlocksContextKey{}, append([]seelectx.PromptBlock(nil), blocks...))
}

// nodePromptBlocksFromContext 读取 ctx 中的节点级 PromptBlocks（拷贝）。
func nodePromptBlocksFromContext(ctx context.Context) []seelectx.PromptBlock {
	if ctx == nil {
		return nil
	}
	blocks, _ := ctx.Value(nodePromptBlocksContextKey{}).([]seelectx.PromptBlock)
	return append([]seelectx.PromptBlock(nil), blocks...)
}
