package contract

import (
	"context"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// Command is the application command registration contract.
type Command interface {
	Name() string
	Description() string
	Execute(context.Context, []string) (CommandResult, error)
}

// CommandResult is the result of one command execution.
type CommandResult struct {
	Notice      string
	Exit        bool
	Interaction *protocol.Interaction
}
