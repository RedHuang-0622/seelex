package input_router

import (
	"context"
	"strings"
)

// Dispatcher 把规范化输入分派到具体用例（command/skill/plugin/conversation）。
type Dispatcher interface {
	Dispatch(context.Context, string) error
}

// Route 是一条分派规则（Matches + Dispatch）。
type Route interface {
	Matches(string) bool
	Dispatch(context.Context, string) error
}

// Router 是组合式输入路由器：按 command → skill → plugin → conversation
// 的策略顺序分派已规范化输入。
type Router struct {
	routes []Route
}

// RouteHandlers 是装配根注入的路由处理闭包。
type RouteHandlers struct {
	Command      func(context.Context, string) error
	Skill        func(context.Context, string, []string, string) error
	Plugin       func(context.Context, string) error
	Conversation func(context.Context, string) error
}

// NewRouter 按固定策略顺序装配路由。
func NewRouter(handlers RouteHandlers) *Router {
	return &Router{routes: []Route{
		commandRoute{dispatch: handlers.Command},
		skillRoute{dispatch: handlers.Skill},
		pluginRoute{dispatch: handlers.Plugin},
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

type skillRoute struct {
	dispatch func(context.Context, string, []string, string) error
}

func (route skillRoute) Matches(input string) bool { return strings.HasPrefix(input, "#") }
func (route skillRoute) Dispatch(ctx context.Context, input string) error {
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "#")))
	if len(parts) == 0 {
		return nil
	}
	return route.dispatch(ctx, parts[0], parts[1:], input)
}

type pluginRoute struct {
	dispatch func(context.Context, string) error
}

func (route pluginRoute) Matches(input string) bool { return strings.HasPrefix(input, "@") }
func (route pluginRoute) Dispatch(ctx context.Context, input string) error {
	name := strings.TrimSpace(strings.TrimPrefix(input, "@"))
	if name == "" {
		return nil
	}
	return route.dispatch(ctx, name)
}

type conversationRoute struct {
	dispatch func(context.Context, string) error
}

func (conversationRoute) Matches(string) bool { return true }
func (route conversationRoute) Dispatch(ctx context.Context, input string) error {
	return route.dispatch(ctx, input)
}
