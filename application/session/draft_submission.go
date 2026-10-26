package session

import (
	"strings"
	"sync"
)

// DraftSubmissionDeps supplies draft target checks and materialization.
type DraftSubmissionDeps struct {
	IsDraftTarget func(sessionID string) bool
	Transition    func() sync.Locker
	IsInView      func(sessionID string) bool
	Materialize   func(firstInput string) error
	NotInView     error
}

// DraftSubmissionApplication coordinates explicit submit for an unmaterialized
// draft.
type DraftSubmissionApplication struct {
	deps DraftSubmissionDeps
}

// NewDraftSubmission constructs the draft-submit application.
func NewDraftSubmission(deps DraftSubmissionDeps) *DraftSubmissionApplication {
	return &DraftSubmissionApplication{deps: deps}
}

// Materialize performs a cheap target check, rechecks ownership under the view
// transition lock, then materializes the trimmed first input.
func (application *DraftSubmissionApplication) Materialize(sessionID, firstInput string) error {
	if !application.deps.IsDraftTarget(sessionID) {
		return nil
	}
	transition := application.deps.Transition()
	transition.Lock()
	defer transition.Unlock()
	if !application.deps.IsInView(sessionID) {
		return application.deps.NotInView
	}
	return application.deps.Materialize(strings.TrimSpace(firstInput))
}
