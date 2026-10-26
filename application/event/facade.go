// Package event is the Application-facing identity facade for the shared
// kernel event implementation.
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
)

// NewEventHub constructs the shared protocol event hub.
func NewEventHub() *EventHub {
	return kernelevent.NewEventHub()
}
