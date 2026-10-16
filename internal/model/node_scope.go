package model

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// node_scope.go —— NodeScope 的**保形别名**（2026-10-16 M2-C4 前置）。
//
// 类型本体的唯一落点已下沉 `contract/dto`（见 contract/dto/node_scope.go 的搬迁说明：
// 权限判据 policy.ClassForNodeScope 要按节点作用域分主体类，而 backend/limit 只允许
// 依赖契约层）。这里保留同名别名，因此本包既有调用方（account / fork / telemetry /
// stream / tools）**零改动**，类型身份也不变（`type X = Y` 不产生新类型）。

// NodeScope 是 Plan 节点的执行作用域（类型本体在 contract/dto）。
type NodeScope = dto.NodeScope

// MainAgentNodeID 是子代理树的合成根节点 ID（主代理；不是真实子代理）。
const MainAgentNodeID = dto.MainAgentNodeID

// WithNodeScope 把节点作用域注入 ctx（子代理执行请求）。
var WithNodeScope = dto.WithNodeScope

// NodeScopeFromContext 读取 ctx 中的节点作用域；无作用域时返回 (零值, false)。
var NodeScopeFromContext = dto.NodeScopeFromContext

// NodeScopeFromContextOrEmpty 读取节点作用域；无作用域时返回空 NodeScope。
var NodeScopeFromContextOrEmpty = dto.NodeScopeFromContextOrEmpty

// 编译期钉子：本包导出的作用域形状就是契约层那一份（不产生第二份实现）。
var (
	_ func(context.Context, NodeScope) context.Context = dto.WithNodeScope
	_ func(context.Context) (NodeScope, bool)          = dto.NodeScopeFromContext
	_ func(context.Context) NodeScope                  = dto.NodeScopeFromContextOrEmpty
)
