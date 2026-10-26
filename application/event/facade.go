// Package event is the Application-facing identity facade for the shared
// Middle protocol event implementation.
package event

import middleevent "github.com/RedHuang-0622/seelex/middle/protocol/event"

type (
	EventKind               = middleevent.EventKind
	Event                   = middleevent.Event
	MessageDelta            = middleevent.MessageDelta
	CompactionProgressState = middleevent.CompactionProgressState
	CompactionProgress      = middleevent.CompactionProgress
	Subscription            = middleevent.Subscription
	ReplayResult            = middleevent.ReplayResult
	Hub                     = middleevent.Hub
	SessionAwareHub         = middleevent.SessionAwareHub
	EventHub                = middleevent.EventHub
)

const (
	EventSnapshotChanged       = middleevent.EventSnapshotChanged
	EventMessageAdded          = middleevent.EventMessageAdded
	EventMessageDelta          = middleevent.EventMessageDelta
	EventToolStarted           = middleevent.EventToolStarted
	EventToolCompleted         = middleevent.EventToolCompleted
	EventSubagentChanged       = middleevent.EventSubagentChanged
	EventSubagentToolStarted   = middleevent.EventSubagentToolStarted
	EventSubagentToolCompleted = middleevent.EventSubagentToolCompleted
	EventRuntimeChanged        = middleevent.EventRuntimeChanged
	EventChatChanged           = middleevent.EventChatChanged
	EventWorkTableChanged      = middleevent.EventWorkTableChanged
	EventTaskChanged           = middleevent.EventTaskChanged
	EventInteractionOpened     = middleevent.EventInteractionOpened
	EventInteractionClosed     = middleevent.EventInteractionClosed
	EventError                 = middleevent.EventError
	EventResyncRequired        = middleevent.EventResyncRequired
	EventExitRequested         = middleevent.EventExitRequested
	EventTeamChanged           = middleevent.EventTeamChanged
	EventTeammateToolStarted   = middleevent.EventTeammateToolStarted
	EventTeammateToolCompleted = middleevent.EventTeammateToolCompleted
	EventCompactionProgress    = middleevent.EventCompactionProgress
	EventViewSessionChanged    = middleevent.EventViewSessionChanged
	EventSessionsChanged       = middleevent.EventSessionsChanged
)

// NewEventHub constructs the shared protocol event hub.
func NewEventHub() *EventHub {
	return middleevent.NewEventHub()
}
