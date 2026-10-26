// Package lifecycle contains application-level drain, cancellation, shutdown and
// fail-fast policies. It depends only on consumer-provided callbacks.
package lifecycle

import "context"

// Deps supplies the lifecycle owner's state and cleanup actions.
type Deps struct {
	SetDraining            func()
	Idle                   func() <-chan struct{}
	AnyRunning             func() bool
	ReadCancels            func() []context.CancelFunc
	CancelChat             func(requestID string) bool
	BeginShutdown          func() bool
	FlushToolRecords       func()
	StopCatalogRefresh     func()
	CloseSessions          func()
	StopLifecycleConsumers func()
	CloseWorkTable         func()
	ShutdownApproval       func()
}

// Application owns lifecycle command ordering, not the underlying session state.
type Application struct {
	deps Deps
}

// New constructs a lifecycle application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// BeginGracefulShutdown stops accepting new input while allowing accepted work to finish.
func (application *Application) BeginGracefulShutdown() {
	application.deps.SetDraining()
}

// WaitForIdle waits until the host reports idle or the context is canceled.
func (application *Application) WaitForIdle(ctx context.Context) error {
	for {
		idle := application.deps.Idle()
		select {
		case <-idle:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// AnyChatRunning reports whether any session still owns a running turn.
func (application *Application) AnyChatRunning() bool {
	return application.deps.AnyRunning()
}

// CancelAllChats cancels every running session.
func (application *Application) CancelAllChats() {
	for _, cancel := range application.deps.ReadCancels() {
		cancel()
	}
}

// CancelChat cancels the current turn, treating requestID as advisory.
func (application *Application) CancelChat(requestID string) bool {
	return application.deps.CancelChat(requestID)
}

// Shutdown performs the fixed, idempotent cleanup sequence.
func (application *Application) Shutdown() {
	if !application.deps.BeginShutdown() {
		return
	}
	application.deps.FlushToolRecords()
	application.deps.StopCatalogRefresh()
	application.deps.CloseSessions()
	application.deps.StopLifecycleConsumers()
	application.deps.CloseWorkTable()
	application.deps.ShutdownApproval()
}
