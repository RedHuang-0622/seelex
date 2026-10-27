package input_router

import (
	"context"
	"strings"
)

// Dispatcher 把规范化输入分派到具体用例（command/plugin/skill/conversation）。
type Dispatcher interface {
	Dispatch(context.Context, string) error
}

// Route 是一条分派规则（Matches + Dispatch）。
type Route interface {
	Matches(string) bool
	Dispatch(context.Context, string) error
}

// Router 是组合式输入路由器：按 command → plugin → skill → conversation
// 的策略顺序分派已规范化输入。
//
// 前缀与用例一一对应（sigil 契约见 application/assembly/completion.go 与
// docs/gui/modules/shell-and-interactions.md）：
//
//	/  可执行入口：命令 + Skill（工具不在此列，只有模型能调用；要显式调用先注册成命令）
//	#  切换插件
//	$  召回 Skill
//
// `@` 手动召唤团队已退场（2026-10-12 J-1a）：那条 teamRoute 随之删除，`@` 不再是
// 输入前缀（字符让给文件指代）。
//
// 前缀互不为前缀，因此顺序不影响命中；固定顺序只是让"哪条规则拥有哪个字符"
// 有唯一事实，新增前缀时不必推敲相对次序。
type Router struct {
	routes []Route
}

// RouteHandlers 是装配根注入的路由处理闭包。
type RouteHandlers struct {
	Command      func(context.Context, string) error
	Plugin       func(context.Context, string) error
	Skill        func(context.Context, string, []string, string) error
	Conversation func(context.Context, string) error
}

// NewRouter 按固定策略顺序装配路由。
func NewRouter(handlers RouteHandlers) *Router {
	return &Router{routes: []Route{
		commandRoute{dispatch: handlers.Command},
		pluginRoute{dispatch: handlers.Plugin},
		skillRoute{dispatch: handlers.Skill},
		conversationRoute{dispatch: handlers.Conversation},
	}}
}

// Dispatch 命中第一条规则后分派；无命中返回 nil。
func (router *Router) Dispatch(ctx context.Context, input string) error {
	for _, route := range router.routes {
		if route.Matches(input) {
			return route.Dispatch(ctx, input)
		}
	}
	return nil
}

type commandRoute struct {
	dispatch func(context.Context, string) error
}

func (route commandRoute) Matches(input string) bool { return strings.HasPrefix(input, "/") }
func (route commandRoute) Dispatch(ctx context.Context, input string) error {
	return route.dispatch(ctx, input)
}

// pluginRoute 拥有 `#`：切换/停用插件（名字原样交给插件面规整）。
type pluginRoute struct {
	dispatch func(context.Context, string) error
}

func (route pluginRoute) Matches(input string) bool { return strings.HasPrefix(input, "#") }
func (route pluginRoute) Dispatch(ctx context.Context, input string) error {
	name := strings.TrimSpace(strings.TrimPrefix(input, "#"))
	if name == "" {
		return nil
	}
	return route.dispatch(ctx, name)
}

// skillRoute 拥有 `$`：召回（激活）一个 Skill；`$name args...` 的剩余字段作为
// Skill 参数，并把原文一并交给下发方（模型输入保持用户原文）。
type skillRoute struct {
	dispatch func(context.Context, string, []string, string) error
}

func (route skillRoute) Matches(input string) bool { return strings.HasPrefix(input, "$") }
func (route skillRoute) Dispatch(ctx context.Context, input string) error {
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "$")))
	if len(parts) == 0 {
		return nil
	}
	return route.dispatch(ctx, parts[0], parts[1:], input)
}

// teamRoute 已随 `@` 手动召唤团队退场（2026-10-12 J-1a）整条删除：`@` 不再是
// 输入前缀，这条路由没有接收方。

type conversationRoute struct {
	dispatch func(context.Context, string) error
}

func (conversationRoute) Matches(string) bool { return true }
func (route conversationRoute) Dispatch(ctx context.Context, input string) error {
	return route.dispatch(ctx, input)
}
