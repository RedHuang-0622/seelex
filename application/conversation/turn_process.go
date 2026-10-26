package conversation

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/react"
)

// TurnUnit is the SessionUnit surface used by turn start.
type TurnUnit interface {
	ChatStateUnit
	SetCancel(context.CancelFunc)
	SetStream(sessioncase.VisibleOutputSink)
}

// PromotionUnit extends TurnUnit with pending queue access.
type PromotionUnit interface {
	TurnUnit
	PendingRequests() []sessioncase.QueuedRequest
	SetRequests([]sessioncase.QueuedRequest)
}

// TurnStatePort supplies request and ChatState operations.
type TurnStatePort interface {
	BeginLocked(ChatStateUnit, string)
	FinishLocked(ChatStateUnit)
	ProjectQueueLocked(QueueProjectionUnit) ([]string, int)
	QueuedRequests([]sessioncase.QueuedRequest) []Request
	Combine([]Request) Request
}

// TurnProcessDeps supplies task, request identity and busy/idle callbacks.
type TurnProcessDeps struct {
	Tasks          *task_context.Coordinator
	TurnState      TurnStatePort
	NextRequestID  func() string
	EffortFor      func(sessionID string) string
	FallbackBudget func(effort string) prompt.ReActBudget
	MarkBusy       func()
	MarkIdle       func()
	AnyChatRunning func() bool
	SetSessionChat func(sessionID string, chat model.ChatState)
}

// StartOptions controls task-progress ordering.
type StartOptions struct {
	MarkBusy                 bool
	TaskProgressBeforeMirror bool
	TaskProgressAfterMirror  bool
}

// Promotion is one promoted queued batch.
type Promotion struct {
	Promoted  bool
	Context   context.Context
	RequestID string
	Request   Request
	UserEvent model.TranscriptEvent
}

// TurnProcessApplication owns the start-side transaction.
type TurnProcessApplication struct {
	deps TurnProcessDeps
}

// NewTurnProcess constructs the turn-process application.
func NewTurnProcess(deps TurnProcessDeps) *TurnProcessApplication {
	return &TurnProcessApplication{deps: deps}
}

// BeginLocked starts one turn under the caller's view lock.
func (application *TurnProcessApplication) BeginLocked(
	parent context.Context,
	sessionID string,
	unit TurnUnit,
	request Request,
	options StartOptions,
) (context.Context, string, model.TranscriptEvent) {
	requestID := application.deps.NextRequestID()
	effort := application.deps.EffortFor(sessionID)
	budget := request.Budget
	if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
		budget = application.deps.FallbackBudget(effort)
	}
	turnContext, cancel := context.WithCancel(parent)
	unit.SetCancel(cancel)
	application.deps.Tasks.StartReActBudgetForLocked(sessionID, requestID, budget)
	previousTask := application.deps.Tasks.CurrentTaskExecutionFor(sessionID)
	previousCheckpoint := model.TaskCheckpoint{}
	if previousTask != nil && task_context.IsContinuableStatus(previousTask.Status) {
		previousCheckpoint = application.deps.Tasks.BuildTaskCheckpointLocked(previousTask)
	}
	taskState := application.deps.Tasks.BeginTaskFor(
		sessionID, requestID, request.DisplayInput, effort, previousTask, previousCheckpoint,
	)
	application.deps.Tasks.ActivateTaskSkillsLocked(taskState, request.Skills)
	userEvent := application.deps.Tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: requestID, Role: "user", Content: request.DisplayInput,
	})
	unit.SetStream(chat.NewVisibleOutputStream(requestID))
	if options.MarkBusy {
		application.deps.MarkBusy()
	}
	application.deps.TurnState.BeginLocked(unit, requestID)
	if options.TaskProgressBeforeMirror {
		application.deps.Tasks.SetTaskStateLocked(requestID, model.TurnProgressing, "Task is in progress.")
	}
	application.deps.SetSessionChat(sessionID, unit.ChatState())
	if options.TaskProgressAfterMirror {
		application.deps.Tasks.SetTaskStateLocked(requestID, model.TurnProgressing, "Task is in progress.")
	}
	return turnContext, requestID, userEvent
}

// PromoteLocked is the single queue decision point after persistence.
func (application *TurnProcessApplication) PromoteLocked(sessionID string, unit PromotionUnit) Promotion {
	pending := application.deps.TurnState.QueuedRequests(unit.PendingRequests())
	if len(pending) == 0 {
		application.deps.TurnState.FinishLocked(unit)
		unit.SetCancel(nil)
		if !application.deps.AnyChatRunning() {
			application.deps.MarkIdle()
		}
		return Promotion{}
	}

	request := application.deps.TurnState.Combine(pending)
	unit.SetRequests(nil)
	application.deps.TurnState.ProjectQueueLocked(unit)
	turnContext, requestID, userEvent := application.BeginLocked(
		context.Background(), sessionID, unit, request, StartOptions{
			TaskProgressBeforeMirror: true,
		},
	)
	return Promotion{
		Promoted: true, Context: turnContext, RequestID: requestID,
		Request: request, UserEvent: userEvent,
	}
}
