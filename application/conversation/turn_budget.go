package conversation

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
)

// ReactBudgetFinalizationInput is the one internal prompt used for the
// reserved text-only delivery turn after a ReAct budget is exhausted.
const ReactBudgetFinalizationInput = contract.ReactBudgetFinalizationMarker + "\n" +
	"The execution budget is exhausted. Do not call any tools again. " +
	"In one final text response, summarize what was actually completed within the budget, " +
	"what remains unfinished, and the evidence collected for both. " +
	"If the task is complete, state the delivered result; if not, state the verified gap. " +
	"Do not claim work that was not done."

// TurnBudgetFinalizerDeps supplies the context, engine-history and delivery
// mechanisms used by the reserved final turn.
type TurnBudgetFinalizerDeps struct {
	SessionIDFromContext func(context.Context) string
	FallbackSessionID    func() string
	BudgetError          func(requestID string) error
	WrapError            func(err error) error
	PrepareContext       func(sessionID, requestID, input string) (string, error)
	Stream               func(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error)
	RemoveInjectedInput  func(sessionID string) error
}

// TurnBudgetFinalizerApplication owns the reserved final-delivery sequence.
type TurnBudgetFinalizerApplication struct {
	deps TurnBudgetFinalizerDeps
}

// NewTurnBudgetFinalizer constructs the budget finalizer.
func NewTurnBudgetFinalizer(deps TurnBudgetFinalizerDeps) *TurnBudgetFinalizerApplication {
	return &TurnBudgetFinalizerApplication{deps: deps}
}

// Finalize performs at most one text-only model call after budget exhaustion.
func (application *TurnBudgetFinalizerApplication) Finalize(
	ctx context.Context,
	requestID string,
	onChunk func(string),
) error {
	sessionID := application.deps.SessionIDFromContext(ctx)
	if sessionID == "" && application.deps.FallbackSessionID != nil {
		sessionID = application.deps.FallbackSessionID()
	}
	budgetErr := application.deps.BudgetError(requestID)
	if budgetErr == nil {
		return nil
	}
	if application.deps.WrapError != nil {
		budgetErr = application.deps.WrapError(budgetErr)
	}
	finalizationInput, prepareErr := application.deps.PrepareContext(
		sessionID, requestID, ReactBudgetFinalizationInput,
	)
	if prepareErr != nil {
		return fmt.Errorf("%w; prepare final delivery context: %v", budgetErr, prepareErr)
	}
	result, streamErr := application.deps.Stream(ctx, sessionID, finalizationInput, onChunk)
	cleanupErr := application.deps.RemoveInjectedInput(sessionID)
	if streamErr != nil {
		return fmt.Errorf("%w; final delivery failed: %v", budgetErr, streamErr)
	}
	if strings.TrimSpace(result) == "" {
		return fmt.Errorf("%w; final delivery returned no text", budgetErr)
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}
