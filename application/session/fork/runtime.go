// Package sessionfork owns durable Session fork orchestration and project
// binding.
package sessionfork

import (
	taskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	sessionruntime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
)

type forkComponents struct {
	sessions *sessionruntime.Coordinator
	tasks    *taskcontext.Coordinator
}

// RuntimeDeps supplies session scope, storage coordinator and lifecycle state.
type RuntimeDeps struct {
	Core                *kernelstate.Core
	Deps                contract.Dependencies
	SessionsRuntime     *sessionruntime.Coordinator
	Tasks               *taskcontext.Coordinator
	Sessions            *sessionstate.Domain
	Closed              func() bool
	Draining            func() bool
	PerSessionExecution func() bool
	NewSessionID        func(string) string
	SetWorkspaceScope   func(string)
	ErrDraining         error
	ErrRunning          error
}

// Runtime owns Session fork decisions and persistence order.
type Runtime struct {
	*kernelstate.Core
	Deps       contract.Dependencies
	components forkComponents

	sessions              *sessionstate.Domain
	closedFn              func() bool
	drainingFn            func() bool
	perSessionExecutionFn func() bool
	newSessionID          func(string) string
	setWorkspaceScope     func(string)
	errDraining           error
	errRunning            error
}

// NewRuntime constructs the Session fork runtime.
func NewRuntime(deps RuntimeDeps) *Runtime {
	return &Runtime{
		Core: deps.Core,
		Deps: deps.Deps,
		components: forkComponents{
			sessions: deps.SessionsRuntime,
			tasks:    deps.Tasks,
		},
		sessions:              deps.Sessions,
		closedFn:              deps.Closed,
		drainingFn:            deps.Draining,
		perSessionExecutionFn: deps.PerSessionExecution,
		newSessionID:          deps.NewSessionID,
		setWorkspaceScope:     deps.SetWorkspaceScope,
		errDraining:           deps.ErrDraining,
		errRunning:            deps.ErrRunning,
	}
}

func (runtime *Runtime) closed() bool {
	return runtime != nil && runtime.closedFn != nil && runtime.closedFn()
}

func (runtime *Runtime) draining() bool {
	return runtime != nil && runtime.drainingFn != nil && runtime.drainingFn()
}

func (runtime *Runtime) perSessionExecution() bool {
	return runtime != nil && runtime.perSessionExecutionFn != nil && runtime.perSessionExecutionFn()
}

func (runtime *Runtime) newGeneratedSessionID(prefix string) string {
	if runtime == nil || runtime.newSessionID == nil {
		return ""
	}
	return runtime.newSessionID(prefix)
}

func (runtime *Runtime) setWorkspaceWriteScope(workspaceID string) {
	if runtime != nil && runtime.setWorkspaceScope != nil {
		runtime.setWorkspaceScope(workspaceID)
	}
}

// ForkSessionLocked performs one durable Session fork while the caller holds
// the per-parent transition lock.
func (runtime *Runtime) ForkSessionLocked(parentID string, request protocol.ForkRequest) (string, error) {
	return runtime.forkSessionLocked(parentID, request)
}
