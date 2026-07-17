package application

import (
	"encoding/json"
	"sync"
)

type EventKind string

const (
	EventSnapshotChanged   EventKind = "snapshot.changed"
	EventMessageAdded      EventKind = "message.added"
	EventMessageDelta      EventKind = "message.delta"
	EventToolStarted       EventKind = "tool.started"
	EventToolCompleted     EventKind = "tool.completed"
	EventRuntimeChanged    EventKind = "runtime.changed"
	EventInteractionOpened EventKind = "interaction.opened"
	EventInteractionClosed EventKind = "interaction.closed"
	EventError             EventKind = "error"
	EventResyncRequired    EventKind = "resync.required"
	EventExitRequested     EventKind = "app.exit_requested"
)

type Event struct {
	Seq       uint64          `json:"seq"`
	Revision  uint64          `json:"revision"`
	RequestID string          `json:"request_id,omitempty"`
	Kind      EventKind       `json:"kind"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Subscription struct {
	Events <-chan Event
	close  func()
}

func (subscription Subscription) Close() {
	if subscription.close != nil {
		subscription.close()
	}
}

type EventHub struct {
	mu          sync.Mutex
	seq         uint64
	nextID      uint64
	subscribers map[uint64]chan Event
}

func NewEventHub() *EventHub { return &EventHub{subscribers: make(map[uint64]chan Event)} }

func (hub *EventHub) Subscribe(buffer int) Subscription {
	if buffer < 1 {
		buffer = 1
	}
	hub.mu.Lock()
	hub.nextID++
	id := hub.nextID
	channel := make(chan Event, buffer)
	hub.subscribers[id] = channel
	hub.mu.Unlock()
	var once sync.Once
	return Subscription{Events: channel, close: func() {
		once.Do(func() {
			hub.mu.Lock()
			if current, ok := hub.subscribers[id]; ok {
				delete(hub.subscribers, id)
				close(current)
			}
			hub.mu.Unlock()
		})
	}}
}

func (hub *EventHub) Publish(kind EventKind, revision uint64, requestID string, payload any) Event {
	var encoded json.RawMessage
	if payload != nil {
		encoded, _ = json.Marshal(payload)
	}
	hub.mu.Lock()
	hub.seq++
	event := Event{Seq: hub.seq, Revision: revision, RequestID: requestID, Kind: kind, Payload: encoded}
	for _, subscriber := range hub.subscribers {
		select {
		case subscriber <- event:
		default:
			for len(subscriber) > 0 {
				<-subscriber
			}
			resync := event
			resync.Kind = EventResyncRequired
			resync.Payload = nil
			subscriber <- resync
		}
	}
	hub.mu.Unlock()
	return event
}
