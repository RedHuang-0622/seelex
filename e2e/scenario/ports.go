package scenario

import (
	"context"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

type Submitter interface {
	Submit(context.Context, string) error
}

type Snapshotter interface {
	Snapshot() application.Snapshot
}

type Subscriber interface {
	Subscribe(int) application.Subscription
}

type InteractionResolver interface {
	Resolve(context.Context, string, string) error
}

type Lifecycle interface {
	Shutdown()
}

// Dependencies is the data-only assembly graph consumed by the scenario runner.
type Dependencies struct {
	Submission  Submitter
	Snapshot    Snapshotter
	Events      Subscriber
	Interaction InteractionResolver
	Lifecycle   Lifecycle
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

type ToolExecutorFactory func(ApprovalRequester) ToolExecutor
