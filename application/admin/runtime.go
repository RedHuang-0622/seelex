package admin

import (
	"context"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

// RuntimeProjectionPort supplies runtime projection collection, view locking
// and publication. The host remains the owner of all state.
type RuntimeProjectionPort interface {
	CollectProjection(context.Context) view_state.RuntimeStateProjection
	LockView()
	ApplyRuntimeProjectionLocked(view_state.RuntimeStateProjection)
	BumpLocked() uint64
	UnlockView()
	CurrentViewSessionID() string
	RuntimeSnapshot() model.RuntimeState
	PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)
	PublishTaskDeltas()
}

// RuntimeDeps contains the single runtime projection host.
type RuntimeDeps struct {
	Projection RuntimeProjectionPort
}

// RuntimeApplication fixes the runtime projection lock and publication order.
type RuntimeApplication struct {
	deps RuntimeDeps
}

// NewRuntime constructs the runtime projection application.
func NewRuntime(deps RuntimeDeps) *RuntimeApplication {
	return &RuntimeApplication{deps: deps}
}

// Refresh collects outside the lock, applies inside it, then publishes after
// unlocking.
func (application *RuntimeApplication) Refresh() {
	port := application.deps.Projection
	projection := port.CollectProjection(context.Background())
	port.LockView()
	port.ApplyRuntimeProjectionLocked(projection)
	revision := port.BumpLocked()
	port.UnlockView()
	port.PublishRuntimeChanged(revision, port.CurrentViewSessionID(), port.RuntimeSnapshot())
	port.PublishTaskDeltas()
}
