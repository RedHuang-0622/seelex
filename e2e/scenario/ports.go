package scenario

import (
	"context"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

type Application interface {
	Submit(context.Context, string) error
	Snapshot() application.Snapshot
	Subscribe(int) application.Subscription
	ResolveInteraction(context.Context, string, string) error
	Shutdown()
}

type ScriptState interface {
	Remaining() int
}

type ToolExecution struct {
	Turn      int
	Name      string
	Arguments string
	Result    string
	Err       error
	Duration  time.Duration
}

type ToolLifecycle interface {
	Started(context.Context, ToolExecution)
	Completed(context.Context, ToolExecution)
}

type ApprovalRequester interface {
	Request(context.Context, application.ApprovalRequest) (application.ApprovalDecision, error)
}

// ToolExecutor runs a scripted tool call against a real tool implementation.
// It keeps scenario fixtures declarative while allowing focused E2E coverage
// of framework-owned tools such as WorkPlan.
type ToolExecutor interface {
	Execute(context.Context, string, string) (string, error)
}

type ToolExecutorFactory func(ApprovalRequester, func(seelebridge.PlanBranchEvent)) ToolExecutor
