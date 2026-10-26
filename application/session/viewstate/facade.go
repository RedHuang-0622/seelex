// Package view_state is the Application-facing identity facade for the shared
// Middle view-state coordinator and its pure projection helpers.
package view_state

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	middlestate "github.com/RedHuang-0622/seelex/middle/view/state"
)

type Coordinator = middlestate.Coordinator
type Deps = middlestate.Deps
type RuntimeStateProjection = middlestate.RuntimeStateProjection

func NewCoordinator(deps Deps) *Coordinator {
	return middlestate.NewCoordinator(deps)
}

func DurableConversationCount(messages []model.Message) int {
	return middlestate.DurableConversationCount(messages)
}
