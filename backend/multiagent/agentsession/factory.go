// Package agentsession builds independent, durable agent session units.
package agentsession

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworksession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// PreparedHistory is the consumer-owned history port used while assembling a
// framework session. The concrete store adapter is injected by composition.
type PreparedHistory interface {
	seelectx.DurableHistory
	SetWorkspaceResolver(func() string)
	PrepareNextLoad([]types.Message)
}

// HistoryFactory creates one prepared history for a session identity.
type HistoryFactory func(sessionID string) PreparedHistory

// Deps contains the host-owned parts of a session template. A caller supplies
// its own Agent/Context/Telemetry template; this package owns the per-session
// identity, model, and optional DurableHistory wiring.
type Deps struct {
	Components        func() frameworksession.SessionComponents
	ModelName         func() string
	NewHistory        HistoryFactory
	WorkspaceResolver func(sessionID string) func() string
	// InitialHistory supplies a durable snapshot to seed the next framework
	// load. It is used when a cold teammate/subagent session resumes after an
	// interruption; nil/empty keeps the ordinary durable-load behavior.
	InitialHistory func(sessionID string) []types.Message
}

// Factory creates framework sessions for subagent and teammate execution.
// It is stateless: registry and lifecycle ownership stay with each domain.
type Factory struct {
	deps Deps
}

// New assembles a factory over host-owned session components.
func New(deps Deps) *Factory {
	return &Factory{deps: deps}
}

// NewWithID creates one session with an explicit identity. When a history
// router is available, the session receives its own DurableHistory keyed by
// that identity; WorkItem and subagent sessions therefore never share the main
// session's context store.
func (factory *Factory) NewWithID(sessionID string, hooks *frameworksession.LoopHooks) (*frameworksession.Session, error) {
	return factory.NewWithIDWithHistory(sessionID, hooks, nil)
}

// NewWithIDWithHistory 创建会话并把一次性的恢复快照交给框架下一次历史加载。
// 普通新会话传 nil；中断恢复传该单元上一份 durable snapshot。
func (factory *Factory) NewWithIDWithHistory(sessionID string, hooks *frameworksession.LoopHooks, initial []types.Message) (*frameworksession.Session, error) {
	if factory == nil || factory.deps.Components == nil {
		return nil, errors.New("agentsession: factory is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("agentsession: session ID is required")
	}
	components := factory.deps.Components()
	components.Hooks = hooks
	components.SessionID = sessionID
	if factory.deps.ModelName != nil {
		components.ModelName = factory.deps.ModelName()
	}
	if factory.deps.NewHistory != nil {
		if history := factory.deps.NewHistory(sessionID); history != nil {
			if factory.deps.WorkspaceResolver != nil {
				history.SetWorkspaceResolver(factory.deps.WorkspaceResolver(sessionID))
			}
			if len(initial) > 0 {
				history.PrepareNextLoad(initial)
			} else if factory.deps.InitialHistory != nil {
				if initial := factory.deps.InitialHistory(sessionID); len(initial) > 0 {
					history.PrepareNextLoad(initial)
				}
			}
			components.History = history
		}
	}
	session, err := frameworksession.NewSession(components)
	if err != nil {
		return nil, fmt.Errorf("agentsession: create session %q: %w", sessionID, err)
	}
	return session, nil
}
