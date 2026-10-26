package input

import (
	"context"
	"strings"
)

// SubmissionDeps supplies the process lifecycle gate and the already assembled
// input dispatcher. Submission does not own command or conversation routing.
type SubmissionDeps struct {
	Lifecycle     func() (closed, draining bool)
	ShutdownError error
	DrainingError error
	Dispatch      func(context.Context, string) error
}

// SubmissionApplication owns the small admission rule shared by all frontend
// input: reject closed/draining processes, ignore blank input, then dispatch.
type SubmissionApplication struct {
	deps SubmissionDeps
}

func NewSubmission(deps SubmissionDeps) *SubmissionApplication {
	return &SubmissionApplication{deps: deps}
}

func (application *SubmissionApplication) Submit(ctx context.Context, text string) error {
	if application == nil {
		return nil
	}
	closed, draining := application.deps.Lifecycle()
	if closed {
		return application.deps.ShutdownError
	}
	if draining {
		return application.deps.DrainingError
	}
	input := strings.TrimSpace(text)
	if input == "" {
		return nil
	}
	return application.deps.Dispatch(ctx, input)
}
