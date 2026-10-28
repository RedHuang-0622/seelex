package conversation

import (
	"context"
	"sync"

	"github.com/RedHuang-0622/seelex/application/event"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// TurnBeginPort starts the authoritative turn transaction under the current
// view lock. TurnProcessApplication is the production implementation.
type TurnBeginPort interface {
	BeginLocked(context.Context, string, TurnUnit, Request, StartOptions) (context.Context, string, model.TranscriptEvent)
}

// TurnStartViewPort is the view-write surface consumed by turn admission.
type TurnStartViewPort interface {
	SessionTitleFor(sessionID string) model.SessionTitle
	SetSessionTitleLocked(sessionID string, title model.SessionTitle)
	AppendMessageWithOriginLocked(role, content string, tool *model.ToolCall, origin model.MessageOrigin) *model.Message
	AppendMessageWithOriginLockedFor(sessionID, role, content string, tool *model.ToolCall, origin model.MessageOrigin) *model.Message
	BumpLocked() uint64
}

// TurnStartDeps supplies admission state and host mechanisms. Business
// ordering remains owned by TurnStartApplication.
type TurnStartDeps struct {
	ViewMu                    *sync.RWMutex
	Lifecycle                 func() (closed, draining bool)
	CurrentSessionIDLocked    func() string
	SessionUnit               func(sessionID string) *sessioncase.SessionUnit
	IsActive                  func(sessionID string) bool
	IsRestoringLocked         func(sessionID string) bool
	IsCompactingLocked        func(sessionID string) bool
	Queue                     *QueueApplication
	Process                   TurnBeginPort
	View                      TurnStartViewPort
	TitleFor                  func(input string) model.SessionTitle
	SetActiveSessionName      func(name string)
	SetSessionChat            func(sessionID string, chat model.ChatState)
	PublishChatState          func(sessionID string)
	SetCurrentTaskBatch       func(sessionID, requestID string)
	PublishRuntimeProjections func()
	DiscardMailbox            func(sessionID string)
	PublishEvent              func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any)
	Dispatch                  func(ctx context.Context, sessionID, requestID string, request Request)
	DeferUntilRestored        func(ctx context.Context, sessionID, input string)
	DeferAfterCompaction      func(ctx context.Context, sessionID, input string)
	Debug                     func(format string, args ...any)
	ClosedError               error
	DrainingError             error
}

// TurnStartApplication owns submission admission and the turn-start
// projection sequence.
type TurnStartApplication struct {
	deps TurnStartDeps
}

// NewTurnStart constructs the turn-start application.
func NewTurnStart(deps TurnStartDeps) *TurnStartApplication {
	return &TurnStartApplication{deps: deps}
}

// StartCurrent starts or queues one request in the currently viewed session.
func (application *TurnStartApplication) StartCurrent(ctx context.Context, request Request) error {
	application.deps.ViewMu.Lock()
	sessionID := application.deps.CurrentSessionIDLocked()
	return application.startLocked(ctx, sessionID, request)
}

// StartFor starts or queues one request for an explicit session.
func (application *TurnStartApplication) StartFor(ctx context.Context, sessionID string, request Request) error {
	application.deps.ViewMu.Lock()
	return application.startLocked(ctx, sessionID, request)
}

func (application *TurnStartApplication) startLocked(
	ctx context.Context,
	sessionID string,
	request Request,
) error {
	if application.deps.Lifecycle != nil {
		closed, draining := application.deps.Lifecycle()
		if closed {
			application.deps.ViewMu.Unlock()
			if application.deps.ClosedError != nil {
				return application.deps.ClosedError
			}
			return context.Canceled
		}
		if draining {
			application.deps.ViewMu.Unlock()
			if application.deps.DrainingError != nil {
				return application.deps.DrainingError
			}
			return context.Canceled
		}
	}
	if application.deps.IsRestoringLocked(sessionID) {
		application.deps.ViewMu.Unlock()
		if application.deps.DeferUntilRestored != nil {
			application.deps.DeferUntilRestored(ctx, sessionID, request.DisplayInput)
		}
		return nil
	}

	unit := application.deps.SessionUnit(sessionID)
	active := application.deps.IsActive(sessionID)
	if unit.ChatState().Running {
		revision := application.deps.Queue.EnqueueLocked(sessionID, unit, request, active)
		application.deps.ViewMu.Unlock()
		application.deps.Queue.AfterEnqueue(sessionID, revision, request)
		return nil
	}
	if application.deps.IsCompactingLocked(sessionID) {
		application.deps.ViewMu.Unlock()
		if application.deps.DeferAfterCompaction != nil {
			application.deps.DeferAfterCompaction(ctx, sessionID, request.DisplayInput)
		}
		return nil
	}
	merged, drained := application.deps.Queue.DrainLocked(unit, request)
	if drained {
		request = merged
		application.deps.SetSessionChat(sessionID, unit.ChatState())
	}

	chatContext, requestID, userEvent := application.deps.Process.BeginLocked(
		ctx, sessionID, unit, request, StartOptions{
			MarkBusy: true, TaskProgressAfterMirror: active,
		},
	)
	application.ensureTitleLocked(sessionID, active, request.DisplayInput)
	user, assistant := application.appendTurnMessagesLocked(sessionID, active, request.DisplayInput, userEvent)
	revision := uint64(0)
	if active {
		revision = application.deps.View.BumpLocked()
	}
	application.deps.ViewMu.Unlock()

	application.publishStart(sessionID, requestID, revision, user, assistant, userEvent)
	if application.deps.Debug != nil {
		application.deps.Debug(
			"startChatFor session=%s request=%s input=%q active=%v",
			sessionID, requestID, request.DisplayInput, active,
		)
	}
	application.deps.Dispatch(chatContext, sessionID, requestID, request)
	return nil
}

func (application *TurnStartApplication) ensureTitleLocked(sessionID string, active bool, input string) {
	if application.deps.View.SessionTitleFor(sessionID).Value != "" {
		return
	}
	title := application.deps.TitleFor(input)
	application.deps.View.SetSessionTitleLocked(sessionID, title)
	if active && application.deps.SetActiveSessionName != nil {
		application.deps.SetActiveSessionName(title.Value)
	}
}

func (application *TurnStartApplication) appendTurnMessagesLocked(
	sessionID string,
	active bool,
	input string,
	userEvent model.TranscriptEvent,
) (model.Message, model.Message) {
	userOrigin := model.MessageOrigin{
		RoleName: userEvent.RoleName, RoleSessionID: userEvent.RoleSessionID,
		RoundID: userEvent.RoundID, UnitSeq: userEvent.UnitSeq, EventSeq: userEvent.Seq,
	}
	mainOrigin := model.MessageOrigin{
		RoleName: model.RoleNameMain, RoleSessionID: sessionID, RoundID: userEvent.RoundID,
	}
	if active {
		return *application.deps.View.AppendMessageWithOriginLocked("user", input, nil, userOrigin),
			*application.deps.View.AppendMessageWithOriginLocked("assistant", "", nil, mainOrigin)
	}
	return *application.deps.View.AppendMessageWithOriginLockedFor(sessionID, "user", input, nil, userOrigin),
		*application.deps.View.AppendMessageWithOriginLockedFor(sessionID, "assistant", "", nil, mainOrigin)
}

func (application *TurnStartApplication) publishStart(
	sessionID, requestID string,
	revision uint64,
	user, assistant model.Message,
	userEvent model.TranscriptEvent,
) {
	application.deps.SetCurrentTaskBatch(sessionID, requestID)
	application.deps.PublishRuntimeProjections()
	application.deps.DiscardMailbox(sessionID)
	application.deps.PublishChatState(sessionID)
	application.deps.PublishEvent(event.EventMessageAdded, revision, requestID, sessionID, user)
	application.deps.PublishEvent(event.EventMessageAdded, revision, requestID, sessionID, assistant)
	application.deps.PublishEvent(event.EventSnapshotChanged, revision, requestID, sessionID, nil)
	_ = userEvent
}
