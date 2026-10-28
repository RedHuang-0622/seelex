package conversation

import (
	"context"
	"sync"

	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
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
	// 输入框图片是**会话视图**上的附件：草稿落成会话的那条路径走 sigil/普通
	// 两条提交口，都不带图（星号路由把输入交给后端路由器，图不属于那条路）。
	request := application.requestFor(sessionID, input, nil)
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
	return application.SubmitForWithImages(ctx, sessionID, input, nil)
}

// SubmitForWithImages 是带图片附件的显式会话提交：图片属于**这一条输入**，
// 随 Request 一起入队/起始，最终写进这条 user 事件行（见 Request.Images）。
func (application *SubmissionApplication) SubmitForWithImages(
	ctx context.Context,
	sessionID, input string,
	images []dto.ComposerImage,
) error {
	return application.deps.Start.StartFor(ctx, sessionID, application.requestFor(sessionID, input, images))
}

func (application *SubmissionApplication) requestFor(sessionID, input string, images []dto.ComposerImage) Request {
	request := application.deps.Request.NewRequestWithImages(input, application.deps.Layers(), images)
	request.Budget = application.deps.BudgetFor(application.deps.EffortFor(sessionID))
	return request
}
