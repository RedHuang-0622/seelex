package task_context

import "context"

// TerminalHandlerDeps supplies the task coordinator operation and the view-plan
// mirroring hooks needed after a terminal tool succeeds.
type TerminalHandlerDeps struct {
	EnsurePlanProjection func(context.Context)
	VerifyAndApply       func(context.Context, string, string) (string, error)
	MirrorTask           func(context.Context)
	MirrorPlan           func(context.Context)
}

type TerminalHandlerApplication struct {
	deps TerminalHandlerDeps
}

func NewTerminalHandler(deps TerminalHandlerDeps) *TerminalHandlerApplication {
	return &TerminalHandlerApplication{deps: deps}
}

// Handler preserves the former core closure order: seed the active view Plan,
// apply the terminal operation, then mirror task and Plan projections only
// after success.
func (application *TerminalHandlerApplication) Handler(kind string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		if application.deps.EnsurePlanProjection != nil {
			application.deps.EnsurePlanProjection(ctx)
		}
		result, err := application.deps.VerifyAndApply(ctx, kind, argsJSON)
		if err == nil {
			if application.deps.MirrorTask != nil {
				application.deps.MirrorTask(ctx)
			}
			if (kind == ToolCheckNode || kind == ToolComplete) && application.deps.MirrorPlan != nil {
				application.deps.MirrorPlan(ctx)
			}
		}
		return result, err
	}
}
