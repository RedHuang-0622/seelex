// Package conversation owns queue editing and chat-state application use
// cases. SessionUnit remains the authoritative state owner.
package conversation

import (
	"errors"
	"sync"

	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switching "github.com/RedHuang-0622/seelex/middle/switch"
)

// QueueUnit is the minimal SessionUnit surface used by queue edits.
type QueueUnit interface {
	QueueProjection() ([]string, int)
	RecallRequest(index int) (sessioncase.QueuedRequest, bool)
	ReorderRequests(from, to int) bool
	ChatState() model.ChatState
	UpdateChat(func(*model.ChatState), *sessioncase.View)
}

// QueueProjectionUnit is the narrower queue projection surface.
type QueueProjectionUnit interface {
	QueueProjection() ([]string, int)
	UpdateChat(func(*model.ChatState), *sessioncase.View)
}

// QueueProjectionPort projects queue state into ChatState.
type QueueProjectionPort interface {
	ProjectQueueLocked(QueueProjectionUnit) ([]string, int)
}

// QueueDeps supplies view lock, lifecycle and publication callbacks.
type QueueDeps struct {
	ViewMu           *sync.RWMutex
	Lifecycle        func() (closed, draining bool)
	ResolveUnit      func(sessionID string) (resolvedID string, unit QueueUnit, err error)
	IsActive         func(sessionID string) bool
	MirrorChatState  func(sessionID string, chat model.ChatState)
	BumpRevision     func() uint64
	PublishSnapshot  func(revision uint64, sessionID string)
	PublishChatState func(sessionID string)
	TurnState        QueueProjectionPort
	DrainingError    error
}

// QueueApplication owns queue edit orchestration.
type QueueApplication struct {
	deps QueueDeps
}

// NewQueue constructs the queue application.
func NewQueue(deps QueueDeps) *QueueApplication {
	return &QueueApplication{deps: deps}
}

// ReorderQueuedInput moves one queued input between projection indexes.
func (application *QueueApplication) ReorderQueuedInput(sessionID string, from, to int) error {
	if application == nil {
		return errors.New("application is not available")
	}
	application.deps.ViewMu.Lock()
	if err := application.guardLocked(); err != nil {
		application.deps.ViewMu.Unlock()
		return err
	}
	resolvedID, unit, err := application.deps.ResolveUnit(sessionID)
	if err != nil {
		application.deps.ViewMu.Unlock()
		return err
	}
	if !unit.ReorderRequests(from, to) {
		application.deps.ViewMu.Unlock()
		return switching.ReorderIndexError(from, to)
	}
	application.applyEditLocked(resolvedID, unit)
	return nil
}

// RecallQueuedInput removes one queued input and returns its text.
func (application *QueueApplication) RecallQueuedInput(sessionID string, index int) (string, error) {
	if application == nil {
		return "", errors.New("application is not available")
	}
	application.deps.ViewMu.Lock()
	if err := application.guardLocked(); err != nil {
		application.deps.ViewMu.Unlock()
		return "", err
	}
	resolvedID, unit, err := application.deps.ResolveUnit(sessionID)
	if err != nil {
		application.deps.ViewMu.Unlock()
		return "", err
	}
	request, ok := unit.RecallRequest(index)
	if !ok {
		application.deps.ViewMu.Unlock()
		return "", switching.RecallIndexError(index)
	}
	application.applyEditLocked(resolvedID, unit)
	return request.DisplayInput, nil
}

func (application *QueueApplication) guardLocked() error {
	if application == nil || application.deps.ViewMu == nil || application.deps.Lifecycle == nil || application.deps.ResolveUnit == nil {
		return errors.New("application is not available")
	}
	closed, draining := application.deps.Lifecycle()
	drainingError := application.deps.DrainingError
	if drainingError == nil {
		drainingError = errors.New("application is draining")
	}
	return switching.QueueEditGate(closed, draining, switching.QueueGateErrors{
		Closed:   errors.New("application is shut down"),
		Draining: drainingError,
	})
}

func (application *QueueApplication) applyEditLocked(sessionID string, unit QueueUnit) {
	application.deps.TurnState.ProjectQueueLocked(unit)
	active := application.deps.IsActive(sessionID)
	revision := uint64(0)
	if active {
		application.deps.MirrorChatState(sessionID, unit.ChatState())
		revision = application.deps.BumpRevision()
	}
	application.deps.ViewMu.Unlock()
	application.deps.PublishSnapshot(revision, sessionID)
	application.deps.PublishChatState(sessionID)
}
