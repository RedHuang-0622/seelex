// Package event implements application event delivery.
package event

import (
	"encoding/json"
	"sync"

	"github.com/RedHuang-0622/seelex/application/model"
)

type EventKind string

const (
	EventSnapshotChanged       EventKind = "snapshot.changed"
	EventMessageAdded          EventKind = "message.added"
	EventMessageDelta          EventKind = "message.delta"
	EventToolStarted           EventKind = "tool.started"
	EventToolCompleted         EventKind = "tool.completed"
	EventSubagentChanged       EventKind = "subagent.changed"
	EventSubagentToolStarted   EventKind = "subagent.tool.started"
	EventSubagentToolCompleted EventKind = "subagent.tool.completed"
	EventRuntimeChanged        EventKind = "runtime.changed"
	EventWorkTableChanged      EventKind = "worktable.changed"
	EventTaskChanged           EventKind = "task.changed"
	EventInteractionOpened     EventKind = "interaction.opened"
	EventInteractionClosed     EventKind = "interaction.closed"
	EventError                 EventKind = "error"
	EventResyncRequired        EventKind = "resync.required"
	EventExitRequested         EventKind = "app.exit_requested"
)

type Event struct {
	ProtocolVersion int    `json:"protocol_version"`
	Seq             uint64 `json:"seq"`
	Revision        uint64 `json:"revision"`
	RequestID       string `json:"request_id,omitempty"`
	// SessionID 是事件所属会话的路由键（M1 起 chat 生命周期事件携带；
	// 空值表示全局事件，SubscribeSession 不过滤）。
	SessionID string          `json:"session_id,omitempty"`
	Kind      EventKind       `json:"kind"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type MessageDelta struct {
	MessageID string `json:"message_id"`
	Delta     string `json:"delta"`
	// ReasoningContent 在回合结束时整段送达（聊天区一行带过，轨迹区完整查看）。
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type Subscription struct {
	Events <-chan Event
	close  func()
}

// Hub 是应用事件投递的窄契约。合约层与核心组件只依赖该接口，
// 不依赖具体投递实现，便于替换、测试与跨进程传输。
type Hub interface {
	Publish(kind EventKind, revision uint64, requestID string, payload any) Event
	Subscribe(buffer int) Subscription
}

// SessionAwareHub 是 Hub 的可选扩展：发布携带会话路由键的事件。
// 装配层提供 *EventHub 时天然满足；测试桩只需实现 Hub 即可，会话发布
// 会退化为普通 Publish（SessionID 为空）。
type SessionAwareHub interface {
	PublishSession(kind EventKind, revision uint64, requestID, sessionID string, payload any) Event
}

// 编译期断言：*EventHub 完整实现 Hub。
var _ Hub = (*EventHub)(nil)
var _ SessionAwareHub = (*EventHub)(nil)

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
	return hub.publish(kind, revision, requestID, "", payload)
}

// PublishSession 与 Publish 等价，但事件携带会话路由键 sessionID，
// 供多会话页签按会话过滤订阅使用。
func (hub *EventHub) PublishSession(kind EventKind, revision uint64, requestID, sessionID string, payload any) Event {
	return hub.publish(kind, revision, requestID, sessionID, payload)
}

func (hub *EventHub) publish(kind EventKind, revision uint64, requestID, sessionID string, payload any) Event {
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
	event := Event{ProtocolVersion: model.ProtocolVersion, Seq: hub.seq, Revision: revision, RequestID: requestID, SessionID: sessionID, Kind: kind, Payload: encoded}
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

// SubscribeSession 返回按会话过滤的订阅：只投递 sessionID 匹配（或全局
// 空 SessionID）的事件。订阅关闭后内部中继与源订阅一并释放。
func (hub *EventHub) SubscribeSession(sessionID string, buffer int) Subscription {
	source := hub.Subscribe(buffer)
	out := make(chan Event, buffer)
	done := make(chan struct{})
	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			source.Close()
			close(done)
		})
	}
	go func() {
		defer close(out)
		for {
			select {
			case event, ok := <-source.Events:
				if !ok {
					return
				}
				if event.SessionID != "" && event.SessionID != sessionID {
					continue
				}
				select {
				case out <- event:
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()
	return Subscription{Events: out, close: closeAll}
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
