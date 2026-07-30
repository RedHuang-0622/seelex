// Package core orchestrates application use cases while depending only on contracts.
package core

import (
	"errors"
)

const defaultHistoryWindow = 200
const maxReplansPerPlanChain = 2

var (
	ErrChatRunning         = errors.New("chat is already running")
	ErrApplicationDraining = errors.New("application is finishing active work")
)

// Service is the public application facade. Stateful responsibilities are
// assembled from focused components; the facade keeps their lifecycle and
// cross-component workflows behind one stable API.
type Service struct {
	*serviceState
	components serviceComponents
}

func New(deps Dependencies) *Service {
	return serviceAssembler{deps: deps}.assemble()
}
