package node

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// NodeScope 是 Plan 节点的执行作用域（类型本体在 internal/model）。
type NodeScope = model.NodeScope

// WithNodeScope 把节点作用域注入 ctx（子代理执行请求）。
func WithNodeScope(ctx context.Context, scope NodeScope) context.Context {
	return model.WithNodeScope(ctx, scope)
}

// NodeScopeFromContext 读取 ctx 中的节点作用域；无作用域时返回 (零值, false)。
func NodeScopeFromContext(ctx context.Context) (NodeScope, bool) {
	return model.NodeScopeFromContext(ctx)
}

// NodeScopeFromContextOrEmpty 读取节点作用域；无作用域时返回空 NodeScope。
func NodeScopeFromContextOrEmpty(ctx context.Context) NodeScope {
	return model.NodeScopeFromContextOrEmpty(ctx)
}
