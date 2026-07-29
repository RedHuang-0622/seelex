package core

import (
	"context"
	"strings"
)

// inputDispatcher is composed into Service so input syntax can gain a new
// route without expanding the Service state machine.
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

func newInputRouter(service *Service) *inputRouter {
	return &inputRouter{routes: []inputRoute{
		commandInputRoute{service: service},
		skillInputRoute{service: service},
		pluginInputRoute{service: service},
		conversationInputRoute{service: service},
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

type commandInputRoute struct{ service *Service }

func (route commandInputRoute) Matches(input string) bool { return strings.HasPrefix(input, "/") }
func (route commandInputRoute) Dispatch(ctx context.Context, input string) error {
	return route.service.submitCommand(ctx, input)
}

type skillInputRoute struct{ service *Service }

func (route skillInputRoute) Matches(input string) bool { return strings.HasPrefix(input, "#") }
func (route skillInputRoute) Dispatch(ctx context.Context, input string) error {
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "#")))
	if len(parts) == 0 {
		return nil
	}
	return route.service.submitSkill(ctx, parts[0], parts[1:], input)
}

type pluginInputRoute struct{ service *Service }

func (route pluginInputRoute) Matches(input string) bool { return strings.HasPrefix(input, "@") }
func (route pluginInputRoute) Dispatch(ctx context.Context, input string) error {
	name := strings.TrimSpace(strings.TrimPrefix(input, "@"))
	if name == "" {
		return nil
	}
	return route.service.SwitchPlugin(ctx, name)
}

type conversationInputRoute struct{ service *Service }

func (conversationInputRoute) Matches(string) bool { return true }
func (route conversationInputRoute) Dispatch(ctx context.Context, input string) error {
	return route.service.submitConversation(ctx, input)
}
