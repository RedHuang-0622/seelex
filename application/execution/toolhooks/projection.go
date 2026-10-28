package toolhooks

import (
	"context"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/event"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

// ProjectionDeps supplies the state owners and host operations consumed by the
// tool projection use case. Branches and ordering stay in the application.
type ProjectionDeps struct {
	Lock                       sync.Locker
	Observe                    func(stage string)
	SessionIDFromContext       func(context.Context) string
	CurrentSessionIDLocked     func() string
	IsActiveLocked             func(sessionID string) bool
	Flush                      func(sessionID string)
	EnsureToolTranscript       func(sessionID, name, id, arguments string)
	AttributeNarration         func(sessionID, name, id, arguments, streamed string)
	StreamedAssistantText      func(sessionID string) string
	AppendToolLocked           func(sessionID, name, id, arguments string, active bool) model.Message
	BumpRevision               func() uint64
	CurrentRequestID           func() string
	BackgroundRequestID        func(sessionID string) string
	CollectProjection          func(sessionID string) view_state.RuntimeStateProjection
	ToolArguments              func(sessionID, id string) string
	RecordToolTranscript       func(sessionID, name, id, arguments, result string, toolErr error) string
	ObserveTool                func(observation task_context.ToolObservation)
	ToolStatus                 func(name string, toolErr error) (dto.ToolEventStatus, string)
	BoundToolResult            func(name, content string) (visible, ref string, truncated bool, totalChars int)
	MutateTool                 func(sessionID, id string, tool *model.ToolCall)
	MirrorActiveView           func()
	RecordReadFile             func(sessionID, arguments string)
	AppendToolResultLocked     func(sessionID, name, id, content, errorText string, status dto.ToolEventStatus, duration time.Duration, ref string, truncated bool, totalChars int) model.Message
	AppendAssistantPlaceholder func(sessionID string) *model.Message
	ApplyProjection            func(sessionID string, projection view_state.RuntimeStateProjection)
	PublishEvent               func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any)
	RecordOutcome              func(sessionID, name string, status dto.ToolEventStatus, toolErr error)
	RefreshWorkTable           func()
	NotifyProjectBoard         func(name string, toolErr error, sessionID string)
	PublishRuntime             func(sessionID, requestID string)
}

// ProjectionApplication owns tool start/complete projection ordering.
type ProjectionApplication struct {
	deps ProjectionDeps
}

// NewProjection constructs the tool projection application.
func NewProjection(deps ProjectionDeps) *ProjectionApplication {
	return &ProjectionApplication{deps: deps}
}

// Start projects one running tool call.
func (application *ProjectionApplication) Start(ctx context.Context, name, id, arguments string) {
	sessionID := application.deps.SessionIDFromContext(ctx)
	if sessionID == "" {
		application.deps.Lock.Lock()
		sessionID = application.deps.CurrentSessionIDLocked()
		application.deps.Lock.Unlock()
	}
	application.deps.Flush(sessionID)
	application.deps.Lock.Lock()
	active := application.deps.IsActiveLocked(sessionID)
	if sessionID == "" {
		sessionID = application.deps.CurrentSessionIDLocked()
	}
	application.deps.EnsureToolTranscript(sessionID, name, id, arguments)
	application.deps.AttributeNarration(
		sessionID, name, id, arguments, application.deps.StreamedAssistantText(sessionID),
	)
	message := application.deps.AppendToolLocked(sessionID, name, id, arguments, active)
	revision := uint64(0)
	if active {
		revision = application.deps.BumpRevision()
	}
	requestID := application.deps.CurrentRequestID()
	if !active {
		requestID = application.deps.BackgroundRequestID(sessionID)
	}
	application.deps.Lock.Unlock()
	application.deps.PublishEvent(event.EventToolStarted, revision, requestID, sessionID, message)
	application.deps.RefreshWorkTable()
	application.deps.PublishRuntime(sessionID, requestID)
}

// Complete projects one finished tool call and its post-lock side effects.
func (application *ProjectionApplication) Complete(
	ctx context.Context,
	name, id, arguments, result string,
	toolErr error,
	duration time.Duration,
	observe func(string),
) {
	emit := func(stage string) {
		if observe != nil {
			observe(stage)
		} else if application.deps.Observe != nil {
			application.deps.Observe(stage)
		}
	}
	sessionID := application.deps.SessionIDFromContext(ctx)
	if sessionID == "" {
		application.deps.Lock.Lock()
		sessionID = application.deps.CurrentSessionIDLocked()
		application.deps.Lock.Unlock()
	}
	emit("toolhook.complete.flush.start")
	application.deps.Flush(sessionID)
	emit("toolhook.complete.flush.done")
	projection := application.deps.CollectProjection(sessionID)
	emit("toolhook.complete.lock.start")
	application.deps.Lock.Lock()
	if sessionID == "" {
		sessionID = application.deps.CurrentSessionIDLocked()
	}
	active := application.deps.IsActiveLocked(sessionID)
	emit("toolhook.complete.lock.done")
	if arguments == "" {
		arguments = application.deps.ToolArguments(sessionID, id)
	}
	emit("toolhook.complete.transcript.start")
	providerResult := application.deps.RecordToolTranscript(sessionID, name, id, arguments, result, toolErr)
	emit("toolhook.complete.transcript.done")
	emit("toolhook.complete.task.start")
	requestID := application.deps.CurrentRequestID()
	if !active {
		requestID = application.deps.BackgroundRequestID(sessionID)
	}
	application.deps.ObserveTool(task_context.ToolObservation{
		RequestID: requestID, Name: name, Result: providerResult, Err: toolErr,
	})
	emit("toolhook.complete.task.done")
	status, errorText := application.deps.ToolStatus(name, toolErr)
	visibleContent, resultRef, truncated, totalChars := application.deps.BoundToolResult(name, result)
	application.deps.MutateTool(sessionID, id, &model.ToolCall{
		ID: id, Name: name, Result: visibleContent, Error: errorText,
		Status: status, Duration: duration,
		ResultRef: resultRef, Truncated: truncated, TotalChars: totalChars,
	})
	application.deps.MirrorActiveView()
	if active && name == "read_file" && toolErr == nil {
		application.deps.RecordReadFile(sessionID, arguments)
	}
	content := visibleContent
	if toolErr != nil {
		content = errorText
	}
	message := application.deps.AppendToolResultLocked(
		sessionID, name, id, content, errorText, status, duration, resultRef, truncated, totalChars,
	)
	assistant := application.deps.AppendAssistantPlaceholder(sessionID)
	emit("toolhook.complete.runtime.start")
	application.deps.ApplyProjection(sessionID, projection)
	emit("toolhook.complete.runtime.done")
	revision := uint64(0)
	if active {
		revision = application.deps.BumpRevision()
	}
	application.deps.Lock.Unlock()
	emit("toolhook.complete.unlock.done")
	emit("toolhook.complete.event.start")
	application.deps.PublishEvent(event.EventToolCompleted, revision, requestID, sessionID, message)
	application.deps.RecordOutcome(sessionID, name, status, toolErr)
	if assistant != nil {
		application.deps.PublishEvent(event.EventMessageAdded, revision, requestID, sessionID, *assistant)
	}
	application.deps.RefreshWorkTable()
	application.deps.NotifyProjectBoard(name, toolErr, sessionID)
	application.deps.PublishRuntime(sessionID, requestID)
	emit("toolhook.complete.event.done")
}
