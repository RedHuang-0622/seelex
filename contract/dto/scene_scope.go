package dto

import "context"

// SceneScope 是一份**后台工作现场**的执行作用域（原 Plan 节点 SceneScope 的替代）：
// 场景身份、角色、分支、工作区与可选 task 绑定。它由 AgentNode.Run 在子代理执行前
// 注入 ctx；权限/可见性、账号选择、路径门、stream 路由与 telemetry 都从 ctx 读取。
//
// 读者按需只取自己的字段：路径门读 WorkspaceID、权限读 SceneID+Role、
// 账号读 SceneID+BranchID+Role、stream 读 Role、telemetry 读 SceneID。
type SceneScope struct {
	SceneID     string
	Role        AccountRole
	BranchID    string
	WorkspaceID string
	// TaskID 是装配件模式绑定的现成 task（B6）：只作为绑定元数据，不进
	// 任何 prompt 块——避免外部 task 内容污染子代理 prompt 格式与处理。
	TaskID string
}

type sceneScopeContextKey struct{}

// MainAgentNodeID 是子代理树的合成根节点 ID（主代理；不是真实子代理）。
const MainAgentNodeID = "main"

// WithSceneScope 把现场作用域注入 ctx（子代理/后台工作单元执行请求）。
func WithSceneScope(ctx context.Context, scope SceneScope) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, sceneScopeContextKey{}, scope)
}

// SceneScopeFromContext 读取 ctx 中的现场作用域；无作用域时返回 (零值, false)。
// 主代理（非子代理）请求不注入作用域，调用方按 false 走主链路逻辑。
func SceneScopeFromContext(ctx context.Context) (SceneScope, bool) {
	if ctx == nil {
		return SceneScope{}, false
	}
	scope, ok := ctx.Value(sceneScopeContextKey{}).(SceneScope)
	return scope, ok
}

// SceneScopeFromContextOrEmpty 读取现场作用域；无作用域时返回空 SceneScope
// （SceneID 为空，可见性策略据此放行主代理全量工具）。
func SceneScopeFromContextOrEmpty(ctx context.Context) SceneScope {
	if scope, ok := SceneScopeFromContext(ctx); ok {
		return scope
	}
	return SceneScope{}
}
