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
	// replay 读取本订阅的重放窗口；nil 表示该订阅未开启窗口（旧语义：溢出即
	// 排空缓冲并投递 resync.required，客户端只能整份重拉快照）。
	replay func(sinceSeq uint64) ReplayResult
	// watermark 返回本订阅已分配的最大 delivery_seq。
	watermark func() uint64
}

// ReplayResult 是一次增量补取的结果，可直接跨 JSON 边界回给消费者。
type ReplayResult struct {
	// Events 是窗口内 delivery_seq > sinceSeq 的事件，按序排列。
	Events []Event `json:"events"`
	// Covered 表示窗口覆盖了 sinceSeq 之后的全部序号；false 表示有事件已被淘
	// 汰（或该订阅未开重放窗口），调用方必须整份重拉快照。
	Covered bool `json:"covered"`
}

// ReplaySince 返回本订阅内 delivery_seq > sinceSeq 且仍留在重放窗口里的事件。
// Covered=false 表示窗口不再覆盖（最早可重放序号 > sinceSeq+1）或该订阅根本没
// 开窗口，调用方必须退回整份快照重拉，不能假设补得齐。
//
// 重放是幂等的：事件可能同时存在于 channel 与窗口中，客户端按 delivery_seq
// 去重即可（已应用过的事件 seq <= lastSeq 直接忽略）。
func (subscription Subscription) ReplaySince(sinceSeq uint64) ReplayResult {
	if subscription.replay == nil {
		return ReplayResult{}
	}
	return subscription.replay(sinceSeq)
}

// DeliveryWatermark 返回本订阅已分配到的最后一个 delivery_seq。调用方用它区分
// "确实没有新事件"与"有新事件但我还没拿到"（后者才需要 ReplaySince 或重拉）。
// 未开窗口的订阅同样可用：水位始终是分配的。
func (subscription Subscription) DeliveryWatermark() uint64 {
	if subscription.watermark == nil {
		return 0
	}
	return subscription.watermark()
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
	// replayWindow > 0 时，每个通过过滤的事件都先进入 replay 环形窗口（按
	// delivery_seq 有序），channel 满时不再丢弃载荷：落后的消费者凭
	// ReplaySince 增量补取，只有窗口被淘汰掉才会退化为整份重拉。
	replay       []Event
	replayWindow int
}

func NewEventHub() *EventHub {
	return &EventHub{subscribers: make(map[uint64]*eventSubscriber)}
}

func (hub *EventHub) Subscribe(buffer int) Subscription {
	return hub.subscribe(nil, buffer, 0)
}

// SubscribeFiltered 返回按谓词筛选的订阅：只有 filter(event) 为真的事件会
// 被投递，且投递序号在筛选后仍然连续（见 Event.DeliverySeq）。
func (hub *EventHub) SubscribeFiltered(filter func(Event) bool, buffer int) Subscription {
	return hub.subscribe(filter, buffer, 0)
}

// SubscribeWithReplay 是带重放窗口的订阅：除 filter 筛选外，最近 replayWindow
// 条事件按 delivery_seq 留在窗口内，供落后消费者 ReplaySince 增量补取。
//
// 与 SubscribeFiltered 的区别只在溢出策略：本订阅缓冲满时**不丢弃载荷、也不
// 排空缓冲**（事件已在窗口里，等消费者自己补），而 SubscribeFiltered 在缓冲满
// 时排空并投递 resync.required，要求消费者重拉整份快照。只推荐给能回报
// delivery_seq 水位并会主动补取的宿主（桌面 Bridge）。replayWindow <= 0 时
// 退化为 SubscribeFiltered。
func (hub *EventHub) SubscribeWithReplay(filter func(Event) bool, buffer, replayWindow int) Subscription {
	if replayWindow <= 0 {
		return hub.subscribe(filter, buffer, 0)
	}
	return hub.subscribe(filter, buffer, replayWindow)
}

func (hub *EventHub) subscribe(filter func(Event) bool, buffer, replayWindow int) Subscription {
	if buffer < 1 {
		buffer = 1
	}
	if replayWindow > 0 {
		// 窗口至少容纳一份缓冲，否则"能补取的范围"小于"可能积压的深度"，
		// 落后一点就直接被淘汰退化重拉。
		if replayWindow < buffer {
			replayWindow = buffer
		}
	}
	hub.mu.Lock()
	hub.nextID++
	id := hub.nextID
	subscriber := &eventSubscriber{
		events: make(chan Event, buffer), filter: filter,
		replay: make([]Event, 0, replayWindow), replayWindow: replayWindow,
	}
	hub.subscribers[id] = subscriber
	hub.mu.Unlock()
	var once sync.Once
	return Subscription{
		Events:    subscriber.events,
		replay:    subscriber.replaySince,
		watermark: subscriber.deliveredWatermark,
		close: func() {
			once.Do(func() {
				hub.mu.Lock()
				if current, ok := hub.subscribers[id]; ok && current == subscriber {
					delete(hub.subscribers, id)
				}
				hub.mu.Unlock()
				subscriber.close()
			})
		},
	}
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
	if subscriber.replayWindow > 0 {
		subscriber.replay = append(subscriber.replay, event)
		if over := len(subscriber.replay) - subscriber.replayWindow; over > 0 {
			subscriber.replay = append(subscriber.replay[:0], subscriber.replay[over:]...)
		}
		select {
		case subscriber.events <- event:
		default:
			// 慢消费者：事件已在重放窗口里，因此既不丢弃载荷也不排空缓冲。
			// 消费者凭 delivery_seq 跳号或 DeliveryWatermark 发现自己落后，
			// 再用 ReplaySince 增量补取；只有窗口被淘汰才退化为整份重拉。
		}
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
		// resync 要求客户端重拉权威快照，必须全局可达：保留会话路由键会让
		// 它被本订阅自己的过滤条件吞掉，客户端从此静默地看旧数据。
		resync.SessionID = ""
		subscriber.events <- resync
	}
}

// replaySince 返回窗口内 delivery_seq > sinceSeq 的事件。Covered=false 表示窗口
// 已不再覆盖该区间（或本订阅未开窗口），调用方必须整份重拉。
func (subscriber *eventSubscriber) replaySince(sinceSeq uint64) ReplayResult {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	if subscriber.closed || subscriber.replayWindow == 0 || len(subscriber.replay) == 0 {
		return ReplayResult{}
	}
	if subscriber.replay[0].DeliverySeq > sinceSeq+1 {
		return ReplayResult{}
	}
	// 顺序扫描定位第一个 > sinceSeq 的序号：窗口按 delivery_seq 严格递增。
	start := len(subscriber.replay)
	for index, item := range subscriber.replay {
		if item.DeliverySeq > sinceSeq {
			start = index
			break
		}
	}
	return ReplayResult{Events: append([]Event(nil), subscriber.replay[start:]...), Covered: true}
}

// deliveredWatermark 返回已分配的最大 delivery_seq（0 = 尚未投递任何事件）。
func (subscriber *eventSubscriber) deliveredWatermark() uint64 {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	return subscriber.seq
}

func (subscriber *eventSubscriber) close() {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	if subscriber.closed {
		return
	}
	subscriber.closed = true
	subscriber.replay = nil
	close(subscriber.events)
}
