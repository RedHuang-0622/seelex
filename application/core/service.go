// Package core orchestrates application use cases while depending only on contracts.
package core

import (
	"context"
	"errors"
	"sync"
)

const defaultHistoryWindow = 200
const maxReplansPerPlanChain = 2

var (
	ErrChatRunning         = errors.New("chat is already running")
	ErrApplicationDraining = errors.New("application is finishing active work")
)

// Service owns the application state machine. Individual use cases live in
// focused files; this file intentionally contains only shared state and setup.
type Service struct {
	mu                  sync.RWMutex
	sessionNameMu       sync.Mutex
	sessionTransitionMu sync.Mutex
	deps                Dependencies
	events              *EventHub
	approval            *ApprovalBroker
	commands            *CommandRegistry
	snapshot            Snapshot
	promptStack         *PromptStack
	effortManager       *EffortManager
	messageSeq          uint64
	cancelChat          context.CancelFunc
	idle                chan struct{}
	draining            bool
	closed              bool
	sessionNames        map[string]sessionNameCacheEntry
	replanInFlight      map[string]struct{}
	inputQueue          []chatRequest
	reactBudget         *activeReActBudget
	taskExecution       *taskExecutionState
	streamOutput        *visibleOutputStream
	inputDispatcher     inputDispatcher
}

func New(deps Dependencies) *Service {
	return serviceAssembler{deps: deps}.assemble()
}
