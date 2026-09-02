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
	// EventChatChanged 下发会话权威聊天运行态（ChatState 载荷）：运行/排队是
	// 后端口径，客户端不得从"收到增量事件"反推。
	EventChatChanged       EventKind = "chat.changed"
	EventWorkTableChanged  EventKind = "worktable.changed"
	EventTaskChanged       EventKind = "task.changed"
	EventInteractionOpened EventKind = "interaction.opened"
	EventInteractionClosed EventKind = "interaction.closed"
	EventError             EventKind = "error"
	EventResyncRequired    EventKind = "resync.required"
	EventExitRequested     EventKind = "app.exit_requested"
)

type Event struct {
	ProtocolVersion int    `json:"protocol_version"`
	Seq             uint64 `json:"seq"`
	// DeliverySeq 是订阅内的投递序号（从 1 起，由 Hub 在投递端赋值）：会话级
	// 订阅在投递端过滤掉其它会话的事件，全局 Seq 必然跳号，因此客户端判定
	// "是否丢了事件"只能看 DeliverySeq —— 缓冲溢出的 ResyncRequired 是唯一
	// 的丢失信号。发布方拿到的返回值不携带该字段。
	DeliverySeq uint64 `json:"delivery_seq,omitempty"`
	Revision    uint64 `json:"revision"`
	RequestID   string `json:"request_id,omitempty"`
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
	// filter 在投递端判定事件是否属于本订阅：不匹配的事件根本不进入 channel，
	// 因此其它会话的流量既不挤占本订阅缓冲，也不要求客户端二次过滤。
	// 谓词由发布 goroutine 执行，必须无阻塞、无副作用（nil = 收全部）。
	filter func(Event) bool
	seq    uint64
}

func NewEventHub() *EventHub {
	return &EventHub{subscribers: make(map[uint64]*eventSubscriber)}
}

func (hub *EventHub) Subscribe(buffer int) Subscription {
	return hub.subscribe(nil, buffer)
}

// SubscribeFiltered 返回按谓词筛选的订阅：只有 filter(event) 为真的事件会
// 被投递，且投递序号在筛选后仍然连续（见 Event.DeliverySeq）。
func (hub *EventHub) SubscribeFiltered(filter func(Event) bool, buffer int) Subscription {
	return hub.subscribe(filter, buffer)
}

func (hub *EventHub) subscribe(filter func(Event) bool, buffer int) Subscription {
	if buffer < 1 {
		buffer = 1
	}
	hub.mu.Lock()
	hub.nextID++
	id := hub.nextID
	subscriber := &eventSubscriber{events: make(chan Event, buffer), filter: filter}
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
// 空 SessionID）的事件。过滤发生在投递端，其它会话的流量不会进入本订阅缓冲。
func (hub *EventHub) SubscribeSession(sessionID string, buffer int) Subscription {
	return hub.SubscribeFiltered(func(event Event) bool {
		return event.SessionID == "" || event.SessionID == sessionID
	}, buffer)
}

func (subscriber *eventSubscriber) deliver(event Event) {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	if subscriber.closed {
		return
	}
	if subscriber.filter != nil && !subscriber.filter(event) {
		return
	}
	subscriber.seq++
	event.DeliverySeq = subscriber.seq
	select {
	case subscriber.events <- event:
	default:
		for len(subscriber.events) > 0 {
			<-subscriber.events
		}
		resync := event
		resync.Kind = EventResyncRequired
		resync.Payload = nil
		// resync 要求客户端重拉权威快照，必须全局可达：保留会话路由键会让
		// 它被本订阅自己的过滤条件吞掉，客户端从此静默地看旧数据。
		resync.SessionID = ""
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
