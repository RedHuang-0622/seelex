package session

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// ApprovalUnit is the per-session approval projection surface.
type ApprovalUnit interface {
	AddApproval(id string)
	RemoveApproval(id string)
}

// InteractionObserverDeps supplies the shared snapshot and approval owner.
type InteractionObserverDeps struct {
	Lock                       sync.Locker
	Snapshot                   *model.Snapshot
	ViewSessionID              func() string
	Unit                       func(sessionID string) ApprovalUnit
	PendingBySession           func(sessionID string) []model.Interaction
	BumpRevision               func() uint64
	PublishEvent               func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any)
	PublishSessionStateChanged func(sessionID string)
}

// InteractionObserverApplication owns approval ownership and the view-slot
// mirroring rule.
type InteractionObserverApplication struct {
	deps InteractionObserverDeps
}

// NewInteractionObserver constructs the approval interaction observer.
func NewInteractionObserver(deps InteractionObserverDeps) *InteractionObserverApplication {
	return &InteractionObserverApplication{deps: deps}
}

// Observe records an opened or closed broker interaction by session.
func (application *InteractionObserverApplication) Observe(
	sessionID, requestID string,
	interaction *model.Interaction,
) {
	sessionID = strings.TrimSpace(sessionID)
	viewSessionID := application.deps.ViewSessionID()
	application.deps.Lock.Lock()
	if interaction == nil {
		if unit := application.unit(sessionID); unit != nil {
			unit.RemoveApproval(requestID)
		}
		slot := application.deps.Snapshot.Interaction
		if slot != nil && (sessionID == "" || (slot.SessionID == sessionID && slot.ID == requestID)) {
			application.deps.Snapshot.Interaction = nil
			application.MirrorLocked(viewSessionID)
		}
	} else {
		copied := *interaction
		copied.SessionID = sessionID
		copied.Options = append([]model.InteractionOption(nil), interaction.Options...)
		if unit := application.unit(sessionID); unit != nil {
			unit.AddApproval(requestID)
		}
		if sessionID == "" || sessionID == viewSessionID {
			application.deps.Snapshot.Interaction = &copied
		}
	}
	revision := application.deps.BumpRevision()
	application.deps.Lock.Unlock()
	publishSessionID := sessionID
	if publishSessionID == "" {
		publishSessionID = viewSessionID
	}
	if interaction == nil {
		application.deps.PublishEvent(event.EventInteractionClosed, revision, requestID, publishSessionID, nil)
		application.deps.PublishSessionStateChanged(publishSessionID)
		return
	}
	application.deps.PublishEvent(event.EventInteractionOpened, revision, requestID, publishSessionID, interaction)
	application.deps.PublishSessionStateChanged(publishSessionID)
}

// MirrorLocked projects the first pending approval for one session. The caller
// holds the shared view lock.
func (application *InteractionObserverApplication) MirrorLocked(sessionID string) {
	if application.deps.PendingBySession == nil {
		application.deps.Snapshot.Interaction = nil
		return
	}
	pending := application.deps.PendingBySession(sessionID)
	if len(pending) == 0 {
		application.deps.Snapshot.Interaction = nil
		return
	}
	copied := pending[0]
	copied.Options = append([]model.InteractionOption(nil), pending[0].Options...)
	application.deps.Snapshot.Interaction = &copied
}

func (application *InteractionObserverApplication) unit(sessionID string) ApprovalUnit {
	if sessionID == "" || application.deps.Unit == nil {
		return nil
	}
	return application.deps.Unit(sessionID)
}
