// Package conversation owns queue editing and chat-state application use
// cases. SessionUnit remains the authoritative state owner.
package conversation

import (
	"errors"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

// QueueUnit is the minimal SessionUnit surface used by queue edits.
type QueueUnit interface {
	Enqueue(sessioncase.QueuedRequest)
	PendingRequests() []sessioncase.QueuedRequest
	SetRequests([]sessioncase.QueuedRequest)
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

// QueueRequestPort restores and combines accepted request payloads.
type QueueRequestPort interface {
	QueuedRequests([]sessioncase.QueuedRequest) []Request
	Combine([]Request) Request
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
	DurableEnqueue   func(sessionID, input string) error
	Debug            func(format string, args ...any)
	NewRequest       func(input string, layers []prompt.PromptLayer) Request
	SessionUnit      func(sessionID string) QueueUnit
	TurnState        QueueProjectionPort
	Requests         QueueRequestPort
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

// EnqueueLocked projects one accepted request into the session queue. The
// caller holds the view transition lock.
func (application *QueueApplication) EnqueueLocked(sessionID string, unit QueueUnit, request Request, active bool) uint64 {
	unit.Enqueue(sessioncase.QueuedRequest{DisplayInput: request.DisplayInput, Payload: request})
	application.deps.TurnState.ProjectQueueLocked(unit)
	application.deps.MirrorChatState(sessionID, unit.ChatState())
	if !active {
		return 0
	}
	return application.deps.BumpRevision()
}

// AfterEnqueue publishes the in-memory queue projection and mirrors it to the
// durable crash-recovery queue. Durable failures never roll back the
// authoritative in-memory queue.
func (application *QueueApplication) AfterEnqueue(sessionID string, revision uint64, request Request) {
	application.deps.PublishSnapshot(revision, sessionID)
	application.deps.PublishChatState(sessionID)
	if application.deps.DurableEnqueue == nil {
		return
	}
	if err := application.deps.DurableEnqueue(sessionID, request.DisplayInput); err != nil && application.deps.Debug != nil {
		application.deps.Debug("durable queue enqueue session=%s err=%v", sessionID, err)
	}
}

// DrainLocked merges recovered pending rows with the current request. The
// caller holds the view transition lock.
func (application *QueueApplication) DrainLocked(unit QueueUnit, request Request) (Request, bool) {
	pending := application.deps.Requests.QueuedRequests(unit.PendingRequests())
	if len(pending) == 0 {
		return request, false
	}
	unit.SetRequests(nil)
	application.deps.TurnState.ProjectQueueLocked(unit)
	return application.deps.Requests.Combine(append(pending, request)), true
}

// RecoverInputs projects durable "sent but unconfirmed" rows back into the
// in-memory queue without starting a turn.
func (application *QueueApplication) RecoverInputs(
	sessionID string,
	resent []dto.QueueItem,
	layers []prompt.PromptLayer,
) {
	if len(resent) == 0 {
		return
	}
	application.deps.ViewMu.Lock()
	unit := application.deps.SessionUnit(sessionID)
	if unit == nil {
		application.deps.ViewMu.Unlock()
		return
	}
	enqueued := 0
	for _, item := range resent {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		unit.Enqueue(sessioncase.QueuedRequest{
			DisplayInput: content,
			Payload:      application.deps.NewRequest(content, layers),
		})
		enqueued++
	}
	if enqueued == 0 {
		application.deps.ViewMu.Unlock()
		return
	}
	application.deps.TurnState.ProjectQueueLocked(unit)
	revision := uint64(0)
	if application.deps.IsActive(sessionID) {
		application.deps.MirrorChatState(sessionID, unit.ChatState())
		revision = application.deps.BumpRevision()
	}
	application.deps.ViewMu.Unlock()
	application.deps.PublishSnapshot(revision, sessionID)
	application.deps.PublishChatState(sessionID)
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
