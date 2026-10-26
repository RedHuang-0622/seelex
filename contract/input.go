package contract

import "context"

// CommandRegistryPort is the command name index consumed by Application.
type CommandRegistryPort interface {
	Register(Command) error
	Get(name string) (Command, bool)
	All() []Command
}

// InputDispatcher is the normalized-input dispatch capability consumed by
// Application.
type InputDispatcher interface {
	Dispatch(context.Context, string) error
}

// InputRouteHandlers are the application callbacks installed into the
// Backend-owned prefix router at composition time.
type InputRouteHandlers struct {
	Command      func(context.Context, string) error
	Plugin       func(context.Context, string) error
	Skill        func(context.Context, string, []string, string) error
	Conversation func(context.Context, string) error
}
