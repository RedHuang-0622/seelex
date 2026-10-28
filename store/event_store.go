package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// EventLogEntry 是执行事实事件库的持久化单元（追加顺序 = 落库顺序）。
// Payload 是 frameworkevent.Event 的 JSON 形态；Seq 由 sink 侧写入方
// （Recorder/planEventSink）保证严格递增，本库不解释其语义。
type EventLogEntry = dto.EventLogEntry

// EventStore 是 seelex 侧执行事实事件库（双轨事件的「事实轨」）：
// 实现 frameworkevent.Sink（Append），把执行事实追加到会话级事件库。
// 前端快照轨由 middle/protocol/event EventHub 承担；本库只负责追加持久化。
// Sink 失败显式返回错误，由调用方 ErrorHandler 隔离 —— 不破坏 WorkPlan
// 或 Agent 的控制流结果（见 Seele event/README.md）。
type EventStore struct {
	router *Router
	mu     sync.Mutex
	// resolverMu 只保护 workspaceResolver 的读写：Append 持有 mu 时会经
	// projectFor 读它，复用 mu 会自死锁。
	// workspaceResolver 返回会话绑定的 workspace（项目作用域）ID；nil 时
	// 回退 Router 当前 active write scope（旧语义，R3 键漂移根因）。
	// 装配方注入 workspace binding 解析器后，执行事实与 record/history
	// 同项目落盘，不再拆到默认项目。
	resolverMu        sync.RWMutex
	workspaceResolver func(sessionID string) string
}

// NewEventStore 创建会话级执行事实事件库（惰性追加，不预读）。
func NewEventStore(router *Router) *EventStore {
	return &EventStore{router: router}
}

// SetWorkspaceResolver 注入会话绑定项目解析器（boot 装配点：从
// workspace.Repo.SessionWorkspace 读绑定）。未注入时回退 active scope。
func (store *EventStore) SetWorkspaceResolver(resolver func(sessionID string) string) {
	if store == nil {
		return
	}
	store.resolverMu.Lock()
	store.workspaceResolver = resolver
	store.resolverMu.Unlock()
}

func (store *EventStore) projectFor(sessionID string) string {
	store.resolverMu.RLock()
	resolver := store.workspaceResolver
	store.resolverMu.RUnlock()
	if resolver != nil {
		// resolver 返回 "" = 未关联会话 → 默认项目。未关联会话的执行事实
		// 不得随 Router 活跃写作用域漂移（否则同一会话的 record/事件被拆到
		// 两个项目，manifest 错键、会话打不开）。
		return resolver(sessionID)
	}
	if store.router != nil {
		return store.router.Workspace()
	}
	return ""
}

// Append 实现 frameworkevent.Sink：按事件 Location（agent.runtime →
// session_id）落到对应会话的事件库。会话标识缺失时跳过持久化
// （事件仍已在内存事件库中，best-effort 语义，不返回错误）。
func (store *EventStore) Append(ctx context.Context, event frameworkevent.Event) error {
	if store == nil || store.router == nil {
		return nil
	}
	sessionID := SessionIDFromEvent(event)
	if sessionID == "" {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("event store: marshal execution event: %w", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.router.AppendFrameworkEventWorkspace(ctx, store.projectFor(sessionID), sessionID, EventLogEntry{
		Seq:     event.Sequence,
		Payload: payload,
	})
}

// Load 读取会话级事件库（按 Seq 排序，追加顺序）。
// 事件库为空时返回空切片（不报错）。
func (store *EventStore) Load(ctx context.Context, sessionID string) ([]frameworkevent.Event, error) {
	return store.loadRange(ctx, sessionID, 0, 0)
}

// LoadRange 读取会话级事件库的 Seq 区间（含端点；按追加顺序排序）。
// fromSeq/toSeq 均传 0 = 全量（兼容 Load 语义）；fromSeq > toSeq 返回显式
// 错误（与 repository.ReadEventRange 同一区间契约，G7 双轨读回用）。
func (store *EventStore) LoadRange(ctx context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error) {
	if fromSeq == 0 && toSeq == 0 {
		return store.loadRange(ctx, sessionID, 0, 0)
	}
	if fromSeq > toSeq {
		return nil, fmt.Errorf("event store: invalid event range %d..%d", fromSeq, toSeq)
	}
	return store.loadRange(ctx, sessionID, fromSeq, toSeq)
}

// loadRange 读取并按范围过滤（from=to=0 表示全量）。
func (store *EventStore) loadRange(ctx context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error) {
	if store == nil || store.router == nil {
		return nil, fmt.Errorf("event store: router is unavailable")
	}
	entries, err := store.router.ReadFrameworkEventsWorkspace(ctx, store.projectFor(sessionID), sessionID)
	if err != nil {
		return nil, err
	}
	events := make([]frameworkevent.Event, 0, len(entries))
	for _, entry := range entries {
		var event frameworkevent.Event
		if err := json.Unmarshal(entry.Payload, &event); err != nil {
			return nil, fmt.Errorf("event store: decode execution event: %w", err)
		}
		if fromSeq == 0 && toSeq == 0 {
			events = append(events, event)
			continue
		}
		if event.Sequence >= fromSeq && event.Sequence <= toSeq {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	return events, nil
}

// SessionIDFromEvent 从事件的 agent.runtime Location 提取会话标识
// （agent.EventLocator.SessionID 的投影；缺失时无法落会话级事件库）。
//
// **这是"从事件读会话归属"的唯一一处判据**（AGENTS §8.3：同一判据只许出现在一处）。
// 三个消费者都问这一句：事件落库前的路由（本文件的 Append）、统一事件查询的话题映射
// （backend/engine/obs 的 UnifiedEventTopic）与持久化前的关联补全
// （obs.CorrelateMainSessionID）。这条判据一旦漂移，事件会"落进 A 会话、
// 被读成 B 会话"，所以不许各写一份（历史上装配层曾有一份逐字副本，
// 2026-10-17 的 P21-9 收敛到此）。
func SessionIDFromEvent(event frameworkevent.Event) string {
	return storesession.SessionIDFromEvent(event)
}

// SessionIDFromLocations 是上面那条判据在 Location 切片上的形态（给只有 Locations 的
// 调用方用；语义与 SessionIDFromEvent 完全一致，两条入口共用下面这一份循环）。
func SessionIDFromLocations(locations []frameworkevent.Location) string {
	return storesession.SessionIDFromLocations(locations)
}
