// Package residency owns the two application-memory LRU orders, protected-set
// evaluation and release orchestration for sessions.
package residency

import (
	"fmt"
	"log"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
	switching "github.com/RedHuang-0622/seelex/middle/switch"
)

type residentEngine interface {
	HasSession(sessionID string) bool
	UnloadSession(sessionID string) error
}

// RuntimeDeps supplies the host reads and side effects needed by residency.
// The runtime owns both LRU orders and their synchronization; SessionUnit and
// View remain owned by their existing domains.
type RuntimeDeps struct {
	Core                  *kernelstate.Core
	Units                 *sessionstate.Domain
	View                  *viewstate.Coordinator
	Port                  applicationsession.ResidencyPort
	ResidentSessionLimit  func() int
	LoadedContentLimit    func() int
	ReleaseWindow         func() int
	FlushSession          func(sessionID string) error
	ReleaseResident       func(sessionID string) error
	RequestCatalogRefresh func()
	ReloadContent         func(sessionID string) error
}

// Runtime owns the resident-engine and loaded-content LRU orders.
type Runtime struct {
	deps               RuntimeDeps
	residentOrder      []string
	loadedContentOrder []string
}

// NewRuntime constructs the Session residency runtime.
func NewRuntime(deps RuntimeDeps) *Runtime {
	return &Runtime{deps: deps}
}

// TouchResident records resident use and reconciles the resident layer.
func (runtime *Runtime) TouchResident(sessionID string) {
	if runtime == nil || sessionID == "" || runtime.deps.Core == nil {
		return
	}
	engine, ok := runtime.deps.Core.Deps.Engine.(residentEngine)
	if !ok || !engine.HasSession(sessionID) {
		return
	}
	runtime.deps.Core.ViewMu.Lock()
	runtime.MarkResidentRecentLocked(sessionID, true)
	runtime.deps.Core.ViewMu.Unlock()
	runtime.ReconcileResident()
}

// MarkResidentRecentLocked moves a resident session to the MRU end.
func (runtime *Runtime) MarkResidentRecentLocked(sessionID string, resident bool) {
	runtime.residentOrder = runtime.deps.Port.Touch(runtime.residentOrder, sessionID)
	if unit := runtime.deps.Units.Unit(sessionID); unit != nil {
		unit.SetResident(resident)
	}
}

// ReconcileResident evicts resident engines until the configured window is met.
func (runtime *Runtime) ReconcileResident() {
	if runtime == nil || runtime.deps.Core == nil {
		return
	}
	if _, ok := runtime.deps.Core.Deps.Engine.(residentEngine); !ok {
		return
	}
	runtime.deps.Port.Evict(applicationsession.EvictionDeps{
		Snapshot: func() applicationsession.EvictionSnapshot {
			runtime.deps.Core.ViewMu.Lock()
			defer runtime.deps.Core.ViewMu.Unlock()
			return runtime.ResidentSnapshotLocked()
		},
		Evict: runtime.deps.ReleaseResident,
		OnEvictError: func(sessionID string, err error) {
			log.Printf("[session-release] engine evict %q: %v", sessionID, err)
		},
	})
}

// ResidentSnapshotLocked returns one coherent resident eviction reading.
func (runtime *Runtime) ResidentSnapshotLocked() applicationsession.EvictionSnapshot {
	engine, ok := runtime.deps.Core.Deps.Engine.(residentEngine)
	if !ok {
		return applicationsession.EvictionSnapshot{}
	}
	snapshot := applicationsession.EvictionSnapshot{
		Order:       append([]string(nil), runtime.residentOrder...),
		Occupied:    make(map[string]bool, len(runtime.residentOrder)),
		Protected:   make(map[string]bool, len(runtime.residentOrder)),
		LegacyLimit: runtime.deps.ResidentSessionLimit(),
		Window:      runtime.deps.ReleaseWindow(),
	}
	for _, sessionID := range runtime.residentOrder {
		if sessionID == "" {
			continue
		}
		unit := runtime.deps.Units.Unit(sessionID)
		if unit == nil || !unit.Resident() || !engine.HasSession(sessionID) {
			continue
		}
		snapshot.Occupied[sessionID] = true
		if runtime.ProtectedSessionLocked(sessionID) {
			snapshot.Protected[sessionID] = true
		}
	}
	return snapshot
}

// SessionBusy reports whether a session must not be released.
func (runtime *Runtime) SessionBusy(unit *sessionstate.SessionUnit) bool {
	return switching.Busy(sessionBusyState(unit))
}

func sessionBusyState(unit *sessionstate.SessionUnit) switching.BusyState {
	if unit == nil {
		return switching.BusyState{}
	}
	chat := unit.ChatState()
	return switching.BusyState{
		PendingApprovals: unit.PendingApprovalCount(),
		Running:          chat.Running,
		QueuedCount:      chat.QueuedCount,
		PendingRequests:  len(unit.PendingRequests()),
	}
}

// TouchContent records visible-content use and reconciles the content layer.
func (runtime *Runtime) TouchContent(sessionID string) {
	if runtime == nil || sessionID == "" {
		return
	}
	runtime.deps.Core.ViewMu.Lock()
	runtime.MarkContentRecentLocked(sessionID)
	runtime.deps.Core.ViewMu.Unlock()
	runtime.ReconcileContent(sessionID)
}

// MarkContentRecentLocked moves a session to the content MRU end.
func (runtime *Runtime) MarkContentRecentLocked(sessionID string) {
	if sessionID == "" {
		return
	}
	runtime.loadedContentOrder = runtime.deps.Port.Touch(runtime.loadedContentOrder, sessionID)
}

// ForgetContentLocked removes a session from the content order.
func (runtime *Runtime) ForgetContentLocked(sessionID string) {
	runtime.loadedContentOrder = runtime.deps.Port.Forget(runtime.loadedContentOrder, sessionID)
}

// PruneContentOrderLocked reconciles the content order with loaded units.
func (runtime *Runtime) PruneContentOrderLocked() int {
	runtime.loadedContentOrder = runtime.deps.Port.Prune(
		runtime.loadedContentOrder,
		runtime.ContentLoadedLocked,
		runtime.deps.Units.UnitIDs(),
	)
	return len(runtime.loadedContentOrder)
}

// ReconcileContent evicts loaded content until the configured window is met.
func (runtime *Runtime) ReconcileContent(protect string) {
	if runtime == nil {
		return
	}
	runtime.deps.Port.Evict(applicationsession.EvictionDeps{
		Snapshot: func() applicationsession.EvictionSnapshot {
			runtime.deps.Core.ViewMu.Lock()
			defer runtime.deps.Core.ViewMu.Unlock()
			return runtime.ContentSnapshotLocked(protect)
		},
		Evict: runtime.EvictLoadedContent,
		OnEvictError: func(sessionID string, err error) {
			log.Printf("[content-lru] evict %q: %v", sessionID, err)
		},
	})
}

// ContentSnapshotLocked returns one coherent loaded-content eviction reading.
func (runtime *Runtime) ContentSnapshotLocked(protect string) applicationsession.EvictionSnapshot {
	runtime.PruneContentOrderLocked()
	snapshot := applicationsession.EvictionSnapshot{
		Order:       append([]string(nil), runtime.loadedContentOrder...),
		Protect:     protect,
		Occupied:    make(map[string]bool, len(runtime.loadedContentOrder)),
		Protected:   make(map[string]bool, len(runtime.loadedContentOrder)),
		LegacyLimit: runtime.deps.LoadedContentLimit(),
		Window:      runtime.deps.ReleaseWindow(),
	}
	for _, sessionID := range runtime.loadedContentOrder {
		if sessionID == "" || !runtime.ContentLoadedLocked(sessionID) {
			continue
		}
		snapshot.Occupied[sessionID] = true
		if runtime.ProtectedSessionLocked(sessionID) {
			snapshot.Protected[sessionID] = true
		}
	}
	return snapshot
}

// EvictLoadedContent flushes and drops one idle session's visible content.
func (runtime *Runtime) EvictLoadedContent(sessionID string) error {
	runtime.deps.Core.ViewMu.RLock()
	unit := runtime.deps.Units.Unit(sessionID)
	protected := runtime.ProtectedSessionLocked(sessionID)
	loaded := runtime.ContentLoadedLocked(sessionID)
	runtime.deps.Core.ViewMu.RUnlock()
	if unit == nil || protected || !loaded {
		return nil
	}
	if err := runtime.deps.FlushSession(sessionID); err != nil {
		return fmt.Errorf("flush session %q before content eviction: %w", sessionID, err)
	}
	runtime.deps.Core.ViewMu.Lock()
	if sessionID == runtime.deps.Core.Snapshot.Session.ID || !runtime.ContentLoadedLocked(sessionID) {
		runtime.deps.Core.ViewMu.Unlock()
		return nil
	}
	runtime.deps.View.SessionViewMutateLocked(sessionID, func(view *sessionstate.View) {
		view.Conversation = nil
		view.ContentUnloaded = true
	})
	runtime.ForgetContentLocked(sessionID)
	runtime.deps.Core.ViewMu.Unlock()
	runtime.deps.RequestCatalogRefresh()
	return nil
}

// ContentLoadedLocked reports whether a session currently owns visible content.
func (runtime *Runtime) ContentLoadedLocked(sessionID string) bool {
	unit := runtime.deps.Units.Unit(sessionID)
	if unit == nil {
		return false
	}
	loaded := false
	unit.View.Read(func(view *sessionstate.View) {
		loaded = !view.ContentUnloaded && len(view.Conversation) > 0
	})
	return loaded
}

// ContentUnloaded reports whether content must be reloaded from durable state.
func (runtime *Runtime) ContentUnloaded(sessionID string) bool {
	unit := runtime.deps.Units.Unit(sessionID)
	if unit == nil {
		return false
	}
	unloaded := false
	unit.View.Read(func(view *sessionstate.View) { unloaded = view.ContentUnloaded })
	return unloaded
}

// EnsureContent reloads content after an automatic eviction.
func (runtime *Runtime) EnsureContent(sessionID string) error {
	if !runtime.ContentUnloaded(sessionID) {
		return nil
	}
	return runtime.deps.ReloadContent(sessionID)
}

// ProtectedSessionLocked reports the single release-protected-set predicate.
func (runtime *Runtime) ProtectedSessionLocked(sessionID string) bool {
	if runtime == nil || sessionID == "" {
		return true
	}
	return switching.Protected(switching.ProtectedInput{
		SessionID:     sessionID,
		ViewSessionID: runtime.deps.Core.Snapshot.Session.ID,
		Restoring:     runtime.deps.View.IsRestoringLocked(sessionID),
		State:         sessionBusyState(runtime.deps.Units.Unit(sessionID)),
	})
}

// ReleaseContentMemory drops only visible content after a release decision.
func (runtime *Runtime) ReleaseContentMemory(sessionID string) {
	if runtime == nil || sessionID == "" {
		return
	}
	runtime.deps.Core.ViewMu.Lock()
	defer runtime.deps.Core.ViewMu.Unlock()
	if sessionID == runtime.deps.Core.Snapshot.Session.ID || !runtime.ContentLoadedLocked(sessionID) {
		return
	}
	runtime.deps.View.SessionViewMutateLocked(sessionID, func(view *sessionstate.View) {
		view.Conversation = nil
		view.ContentUnloaded = true
	})
	runtime.ForgetContentLocked(sessionID)
}

// ForgetResidentLocked removes a session from the resident order.
func (runtime *Runtime) ForgetResidentLocked(sessionID string) {
	runtime.residentOrder = runtime.deps.Port.Forget(runtime.residentOrder, sessionID)
	if unit := runtime.deps.Units.Unit(sessionID); unit != nil {
		unit.SetResident(false)
	}
}

// ResidentOrderLen returns the resident order length for tests and metrics.
func (runtime *Runtime) ResidentOrderLen() int { return len(runtime.residentOrder) }

// ContentOrderLen returns the content order length for tests and metrics.
func (runtime *Runtime) ContentOrderLen() int { return len(runtime.loadedContentOrder) }
