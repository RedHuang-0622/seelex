package adapters

import (
	inputrouter "github.com/RedHuang-0622/seelex/backend/engine/input"
	"github.com/RedHuang-0622/seelex/contract"
)

// NewCommandRegistryFactory returns the Backend-owned command registry
// constructor without exposing its concrete type to Application.
func NewCommandRegistryFactory() func() contract.CommandRegistryPort {
	return func() contract.CommandRegistryPort {
		return inputrouter.NewCommandRegistry()
	}
}

// NewInputRouterFactory returns the Backend-owned normalized-input router.
func NewInputRouterFactory() func(contract.InputRouteHandlers) contract.InputDispatcher {
	return func(handlers contract.InputRouteHandlers) contract.InputDispatcher {
		return inputrouter.NewRouter(inputrouter.RouteHandlers{
			Command:      handlers.Command,
			Plugin:       handlers.Plugin,
			Skill:        handlers.Skill,
			Conversation: handlers.Conversation,
		})
	}
}
