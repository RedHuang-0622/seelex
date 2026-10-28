package conversation

import (
	"context"
	"sync"

	"github.com/RedHuang-0622/seelex/application/prompt"
)

// TurnStarter is the admission boundary consumed by submissions.
type TurnStarter interface {
	StartFor(ctx context.Context, sessionID string, request Request) error
}

// SubmissionDeps supplies request policy and the view transition needed only
// when the currently viewed draft is materialized.
type SubmissionDeps struct {
	TransitionLock   func() sync.Locker
	CurrentSessionID func() string
	MaterializeDraft func(input string) error
	Request          *RequestApplication
	Layers           func() []prompt.PromptLayer
	EffortFor        func(sessionID string) string
	BudgetFor        func(effort string) prompt.ReActBudget
	Start            TurnStarter
}

// SubmissionApplication owns accepted-input construction and draft
// materialization before turn admission.
type SubmissionApplication struct {
	deps SubmissionDeps
}

// NewSubmission constructs the submission application.
func NewSubmission(deps SubmissionDeps) *SubmissionApplication {
	return &SubmissionApplication{deps: deps}
}

// Submit materializes the viewed draft and submits to that session.
func (application *SubmissionApplication) Submit(ctx context.Context, input string) error {
	sessionID := application.deps.CurrentSessionID()
	request := application.requestFor(sessionID, input)
	lock := application.deps.TransitionLock()
	lock.Lock()
	err := application.deps.MaterializeDraft(request.DisplayInput)
	lock.Unlock()
	if err != nil {
		return err
	}
	return application.deps.Start.StartFor(ctx, sessionID, request)
}

// SubmitFor submits to an explicit background session without consuming the
// viewed draft.
func (application *SubmissionApplication) SubmitFor(ctx context.Context, sessionID, input string) error {
	return application.deps.Start.StartFor(ctx, sessionID, application.requestFor(sessionID, input))
}

func (application *SubmissionApplication) requestFor(sessionID, input string) Request {
	request := application.deps.Request.NewRequest(input, application.deps.Layers())
	request.Budget = application.deps.BudgetFor(application.deps.EffortFor(sessionID))
	return request
}
