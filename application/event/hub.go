// Package event implements application event delivery.
package event

import (
	"encoding/json"
	"sync"

	"github.com/RedHuang-0622/seelex/application/model"
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
	ProtocolVersion int             `json:"protocol_version"`
	Seq             uint64          `json:"seq"`
	Revision        uint64          `json:"revision"`
	RequestID       string          `json:"request_id,omitempty"`
	Kind            EventKind       `json:"kind"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

type MessageDelta struct {
	MessageID string `json:"message_id"`
	Delta     string `json:"delta"`
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
	publishMu   sync.Mutex
	seq         uint64
	nextID      uint64
	subscribers map[uint64]*eventSubscriber
}

type eventSubscriber struct {
	mu     sync.Mutex
	events chan Event
	closed bool
}

func NewEventHub() *EventHub {
	return &EventHub{subscribers: make(map[uint64]*eventSubscriber)}
}

func (hub *EventHub) Subscribe(buffer int) Subscription {
	if buffer < 1 {
		buffer = 1
	}
	hub.mu.Lock()
	hub.nextID++
	id := hub.nextID
	subscriber := &eventSubscriber{events: make(chan Event, buffer)}
	hub.subscribers[id] = subscriber
	hub.mu.Unlock()
	var once sync.Once
	return Subscription{Events: subscriber.events, close: func() {
		once.Do(func() {
			hub.mu.Lock()
			if current, ok := hub.subscribers[id]; ok && current == subscriber {
				delete(hub.subscribers, id)
			}
			hub.mu.Unlock()
			subscriber.close()
		})
	}}
}

func (hub *EventHub) Publish(kind EventKind, revision uint64, requestID string, payload any) Event {
	var encoded json.RawMessage
	if payload != nil {
		encoded, _ = json.Marshal(payload)
	}
	// Preserve global event order without holding the subscriber-registry lock
	// during delivery. Subscribe and Close therefore remain independent from a
	// slow subscriber, while concurrent publishers still observe monotonic seq.
	hub.publishMu.Lock()
	defer hub.publishMu.Unlock()

	hub.mu.Lock()
	hub.seq++
	event := Event{ProtocolVersion: model.ProtocolVersion, Seq: hub.seq, Revision: revision, RequestID: requestID, Kind: kind, Payload: encoded}
	subscribers := make([]*eventSubscriber, 0, len(hub.subscribers))
	for _, subscriber := range hub.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	hub.mu.Unlock()

	for _, subscriber := range subscribers {
		subscriber.deliver(event)
	}
	return event
}

func (subscriber *eventSubscriber) deliver(event Event) {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	if subscriber.closed {
		return
	}
	select {
	case subscriber.events <- event:
	default:
		for len(subscriber.events) > 0 {
			<-subscriber.events
		}
		resync := event
		resync.Kind = EventResyncRequired
		resync.Payload = nil
		subscriber.events <- resync
	}
}

func (subscriber *eventSubscriber) close() {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	if subscriber.closed {
		return
	}
	subscriber.closed = true
	close(subscriber.events)
}
