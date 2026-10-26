// Package task_context is the Application-facing identity facade for the
// Engine-owned task execution and ledger helpers.
package task_context

import (
	"context"

	taskcontext "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type (
	ChatEndSummary    = taskcontext.ChatEndSummary
	ModelOutput       = taskcontext.ModelOutput
	Coordinator       = taskcontext.Coordinator
	Deps              = taskcontext.Deps
	PromptPort        = taskcontext.PromptPort
	RestoredTaskState = taskcontext.RestoredTaskState
	ToolObservation   = taskcontext.ToolObservation
	ContextBudget     = taskcontext.ContextBudget
)

const (
	ToolComplete  = taskcontext.ToolComplete
	ToolCheckNode = taskcontext.ToolCheckNode
)

func ActivePlanFrame(stack []model.SessionPlanFrame, activeID string) *model.SessionPlanFrame {
	return taskcontext.ActivePlanFrame(stack, activeID)
}

func ActivePlanFromStack(stack []model.SessionPlanFrame, activeID string) *model.PlanState {
	return taskcontext.ActivePlanFromStack(stack, activeID)
}

func SessionIDFromContext(ctx context.Context) string {
	return taskcontext.SessionIDFromContext(ctx)
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return taskcontext.WithSessionID(ctx, sessionID)
}

func NewCoordinator(deps Deps) *Coordinator {
	return taskcontext.NewCoordinator(deps)
}

func TranscriptTailHistory(events []model.TranscriptEvent, tokenBudget, maxUnits int) []contract.EngineMessage {
	return taskcontext.TranscriptTailHistory(events, tokenBudget, maxUnits)
}

func LedgerIdentity(record dto.TaskRecord) string {
	return taskcontext.LedgerIdentity(record)
}

func RetainedFromForCompactions(events []model.TranscriptEvent, compactions []model.ContextCompaction) int {
	return taskcontext.RetainedFromForCompactions(events, compactions)
}

func ContextBudgetFor(runtime any) ContextBudget {
	return taskcontext.ContextBudgetFor(runtime)
}
