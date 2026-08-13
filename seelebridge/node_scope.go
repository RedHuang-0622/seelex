package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/node"
)

// NodeScope 是 Plan 节点的执行作用域；类型本体已下沉 internal/model，
// 根包保留兼容别名（WithNodeScope / NodeScopeFromContext 为薄包装）。
type NodeScope = model.NodeScope

// WithNodeScope 把节点作用域注入 ctx（子代理执行请求）。
func WithNodeScope(ctx context.Context, scope NodeScope) context.Context {
	return model.WithNodeScope(ctx, scope)
}

// NodeScopeFromContext 读取 ctx 中的节点作用域；无作用域时返回 (零值, false)。
func NodeScopeFromContext(ctx context.Context) (NodeScope, bool) {
	return model.NodeScopeFromContext(ctx)
}

// nodeScopeFromContextOrEmpty 读取节点作用域；无作用域时返回空 NodeScope。
func nodeScopeFromContextOrEmpty(ctx context.Context) NodeScope {
	return model.NodeScopeFromContextOrEmpty(ctx)
}

// withNodePromptBlocks 把节点级 PromptBlocks（目标 + 父证据 + 预算）注入 ctx
// （实现下沉 seelebridge/node，本包装供根包装配器与测试使用）。
func withNodePromptBlocks(ctx context.Context, blocks []seelectx.PromptBlock) context.Context {
	return node.WithNodePromptBlocks(ctx, blocks)
}

// nodePromptBlocksFromContext 读取 ctx 中的节点级 PromptBlocks（拷贝）。
func nodePromptBlocksFromContext(ctx context.Context) []seelectx.PromptBlock {
	return node.NodePromptBlocksFromContext(ctx)
}
