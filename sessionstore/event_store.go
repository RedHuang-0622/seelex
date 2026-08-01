package sessionstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// EventLogEntry 是执行事实事件库的持久化单元（追加顺序 = 落库顺序）。
// Payload 是 frameworkevent.Event 的 JSON 形态；Seq 由 sink 侧写入方
// （Recorder/planEventSink）保证严格递增，本库不解释其语义。
type EventLogEntry struct {
	Seq     uint64          `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// EventStore 是 seelex 侧执行事实事件库（双轨事件的「事实轨」）：
// 实现 frameworkevent.Sink（Append），把执行事实追加到会话级事件库。
// 前端快照轨由 application/event EventHub 承担；本库只负责追加持久化。
// Sink 失败显式返回错误，由调用方 ErrorHandler 隔离 —— 不破坏 WorkPlan
// 或 Agent 的控制流结果（见 Seele event/README.md）。
type EventStore struct {
	router *Router
	mu     sync.Mutex
}

// NewEventStore 创建会话级执行事实事件库（惰性追加，不预读）。
func NewEventStore(router *Router) *EventStore {
	return &EventStore{router: router}
}

// Append 实现 frameworkevent.Sink：按事件 Location（agent.runtime →
// session_id）落到对应会话的事件库。会话标识缺失时跳过持久化
// （事件仍已在内存事件库中，best-effort 语义，不返回错误）。
func (store *EventStore) Append(ctx context.Context, event frameworkevent.Event) error {
	if store == nil || store.router == nil {
		return nil
	}
	sessionID := sessionIDFromEvent(event)
	if sessionID == "" {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("event store: marshal execution event: %w", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.router.AppendFrameworkEvent(ctx, sessionID, EventLogEntry{
		Seq:     event.Sequence,
		Payload: payload,
	})
}

// Load 读取会话级事件库（按 Seq 排序，追加顺序）。
// 事件库为空时返回空切片（不报错）。
func (store *EventStore) Load(ctx context.Context, sessionID string) ([]frameworkevent.Event, error) {
	if store == nil || store.router == nil {
		return nil, fmt.Errorf("event store: router is unavailable")
	}
	entries, err := store.router.ReadFrameworkEvents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	events := make([]frameworkevent.Event, 0, len(entries))
	for _, entry := range entries {
		var event frameworkevent.Event
		if err := json.Unmarshal(entry.Payload, &event); err != nil {
			return nil, fmt.Errorf("event store: decode execution event: %w", err)
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	return events, nil
}

// sessionIDFromEvent 从事件的 agent.runtime Location 提取会话标识
// （agent.EventLocator.SessionID 的投影；缺失时无法落会话级事件库）。
func sessionIDFromEvent(event frameworkevent.Event) string {
	for _, location := range event.Locations {
		if location.Kind != "agent.runtime" {
			continue
		}
		if sessionID := location.IDs["session_id"]; sessionID != "" {
			return sessionID
		}
	}
	return ""
}
