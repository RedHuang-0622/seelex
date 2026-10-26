package session

import (
	"strings"
	"sync"

	switching "github.com/RedHuang-0622/seelex/middle/switch"
)

// LifecycleDeps supplies unload transition, persistence, release and view
// callbacks.
type LifecycleDeps struct {
	Transition      func(sessionID string) sync.Locker
	Gate            func(sessionID string) switching.UnloadGate
	IsViewSession   func(sessionID string) bool
	Persist         func(sessionID string) error
	Release         func(sessionID string) error
	RenewView       func()
	PublishSnapshot func(sessionID string)
	RefreshCatalog  func()
}

// LifecycleApplication owns session unload orchestration. Admission remains in
// middle/switch.Unload.
type LifecycleApplication struct {
	deps LifecycleDeps
}

// NewLifecycle constructs the session lifecycle application.
func NewLifecycle(deps LifecycleDeps) *LifecycleApplication {
	return &LifecycleApplication{deps: deps}
}

// Unload persists a non-view session, releases its memory and renews the view
// when needed.
func (application *LifecycleApplication) Unload(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	transition := application.deps.Transition(sessionID)
	transition.Lock()
	defer transition.Unlock()
	return switching.Unload(switching.UnloadDeps{
		Gate:                   func() switching.UnloadGate { return application.deps.Gate(sessionID) },
		IsViewSession:          application.deps.IsViewSession,
		Persist:                application.deps.Persist,
		Release:                application.deps.Release,
		RenewView:              application.deps.RenewView,
		PublishSnapshotChanged: application.deps.PublishSnapshot,
		RefreshCatalog:         application.deps.RefreshCatalog,
	}, sessionID)
}
