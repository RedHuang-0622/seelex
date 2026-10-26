// Package event is the Middle compatibility surface for the shared kernel
// event hub. The implementation lives in kernel/event.
package event

import kernelevent "github.com/RedHuang-0622/seelex/kernel/event"

type (
	EventKind               = kernelevent.EventKind
	Event                   = kernelevent.Event
	MessageDelta            = kernelevent.MessageDelta
	CompactionProgressState = kernelevent.CompactionProgressState
	CompactionProgress      = kernelevent.CompactionProgress
	Subscription            = kernelevent.Subscription
	ReplayResult            = kernelevent.ReplayResult
	Hub                     = kernelevent.Hub
	SessionAwareHub         = kernelevent.SessionAwareHub
	EventHub                = kernelevent.EventHub
)

const (
	EventSnapshotChanged       = kernelevent.EventSnapshotChanged
	EventMessageAdded          = kernelevent.EventMessageAdded
	EventMessageDelta          = kernelevent.EventMessageDelta
	EventToolStarted           = kernelevent.EventToolStarted
	EventToolCompleted         = kernelevent.EventToolCompleted
	EventSubagentChanged       = kernelevent.EventSubagentChanged
	EventSubagentToolStarted   = kernelevent.EventSubagentToolStarted
	EventSubagentToolCompleted = kernelevent.EventSubagentToolCompleted
	EventRuntimeChanged        = kernelevent.EventRuntimeChanged
	EventChatChanged           = kernelevent.EventChatChanged
	EventWorkTableChanged      = kernelevent.EventWorkTableChanged
	EventTaskChanged           = kernelevent.EventTaskChanged
	EventInteractionOpened     = kernelevent.EventInteractionOpened
	EventInteractionClosed     = kernelevent.EventInteractionClosed
	EventError                 = kernelevent.EventError
	EventResyncRequired        = kernelevent.EventResyncRequired
	EventExitRequested         = kernelevent.EventExitRequested
	EventTeamChanged           = kernelevent.EventTeamChanged
	EventTeammateToolStarted   = kernelevent.EventTeammateToolStarted
	EventTeammateToolCompleted = kernelevent.EventTeammateToolCompleted
	EventCompactionProgress    = kernelevent.EventCompactionProgress
	EventViewSessionChanged    = kernelevent.EventViewSessionChanged
	EventSessionsChanged       = kernelevent.EventSessionsChanged

	CompactionProgressRunning = kernelevent.CompactionProgressRunning
	CompactionProgressDone    = kernelevent.CompactionProgressDone
	CompactionProgressFailed  = kernelevent.CompactionProgressFailed
	CompactionPhaseBegin      = kernelevent.CompactionPhaseBegin
)

// KindIsProcessClass reports whether a kind is process-scoped.
func KindIsProcessClass(kind EventKind) bool {
	return kernelevent.KindIsProcessClass(kind)
}

// KindIsSessionClass reports whether a kind is session-scoped.
func KindIsSessionClass(kind EventKind) bool {
	return kernelevent.KindIsSessionClass(kind)
}

// ValidateSessionRouting validates a kind/session routing pair.
func ValidateSessionRouting(kind EventKind, sessionID string) error {
	return kernelevent.ValidateSessionRouting(kind, sessionID)
}

// NewEventHub constructs the shared kernel event hub.
func NewEventHub() *EventHub {
	return kernelevent.NewEventHub()
}
