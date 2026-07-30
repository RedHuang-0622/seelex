package core

import (
	"context"
	"strings"
)

// inputDispatcher routes syntax through narrow use-case functions. It knows
// nothing about Service or its shared state.
type inputDispatcher interface {
	Dispatch(context.Context, string) error
}

type inputRoute interface {
	Matches(string) bool
	Dispatch(context.Context, string) error
}

type inputRouter struct {
	routes []inputRoute
}

type inputRouteHandlers struct {
	command      func(context.Context, string) error
	skill        func(context.Context, string, []string, string) error
	plugin       func(context.Context, string) error
	conversation func(context.Context, string) error
}

func newInputRouter(handlers inputRouteHandlers) *inputRouter {
	return &inputRouter{routes: []inputRoute{
		commandInputRoute{dispatch: handlers.command},
		skillInputRoute{dispatch: handlers.skill},
		pluginInputRoute{dispatch: handlers.plugin},
		conversationInputRoute{dispatch: handlers.conversation},
	}}
}

func (router *inputRouter) Dispatch(ctx context.Context, input string) error {
	for _, route := range router.routes {
		if route.Matches(input) {
			return route.Dispatch(ctx, input)
		}
	}
	return nil
}

type commandInputRoute struct {
	dispatch func(context.Context, string) error
}

func (route commandInputRoute) Matches(input string) bool { return strings.HasPrefix(input, "/") }
func (route commandInputRoute) Dispatch(ctx context.Context, input string) error {
	return route.dispatch(ctx, input)
}

type skillInputRoute struct {
	dispatch func(context.Context, string, []string, string) error
}

func (route skillInputRoute) Matches(input string) bool { return strings.HasPrefix(input, "#") }
func (route skillInputRoute) Dispatch(ctx context.Context, input string) error {
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "#")))
	if len(parts) == 0 {
		return nil
	}
	return route.dispatch(ctx, parts[0], parts[1:], input)
}

type pluginInputRoute struct {
	dispatch func(context.Context, string) error
}

func (route pluginInputRoute) Matches(input string) bool { return strings.HasPrefix(input, "@") }
func (route pluginInputRoute) Dispatch(ctx context.Context, input string) error {
	name := strings.TrimSpace(strings.TrimPrefix(input, "@"))
	if name == "" {
		return nil
	}
	return route.dispatch(ctx, name)
}

type conversationInputRoute struct {
	dispatch func(context.Context, string) error
}

func (conversationInputRoute) Matches(string) bool { return true }
func (route conversationInputRoute) Dispatch(ctx context.Context, input string) error {
	return route.dispatch(ctx, input)
}
