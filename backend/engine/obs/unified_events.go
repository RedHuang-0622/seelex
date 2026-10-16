package obs

// unified_events.go 是**统一事件库的查询与 B 类摘要日志**（P21-6「域方法随类型走」：
// 从 `backend/engine/assemble/events_unified.go` 归位到观测域；plan §2.5 的
// `events_unified.go` → `backend/engine/obs/`，与 `request_log.go` / `session_telemetry.go` 同域）。
//
// 形态（解耦方案 §02.3 / §04.7，见 docs/2026-08-14-decoupling/06-unified-event-store-decision.md）：
// A 类（plan/节点执行事实）全量持久化是唯一事实源；B 类（llm/tool 意图-效果遥测）留内存实时面，
// 另以有界脱敏摘要追加进**同一** store 事件库（B 摘要与 A 类同库，查询统一、存储不双写）。
// 统一查询按 sessionID/nodeID 关联两轨，用引用/索引而不是复制 payload。
//
// 宿主面只有两处读数：store 事件库的 Router（本层自己造 `store.EventStore`）与内存 tracer 的
// 查询口——后者由宿主以函数值端口传入（域包因此不必认识 tracer 的具体类型）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"

	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	"github.com/RedHuang-0622/seelex/store"
)

// SummaryEventSource 是 B 类摘要事件在统一事件库（store 事件库）中的**稳定 Source 标识**，
// 与 A 类事实（workplan.runner / seelex.subagent）同库共存。
// 导出理由：它是**持久化事实**（落库事件的 Source 字段），查询侧与用例都要按它认事件。
const SummaryEventSource = "seelex.telemetry.summary"

// ErrUnifiedEventsRouterMissing 是"统一事件视图依赖的 store Router 未装配"的**唯一文案处**：
// UnifiedEvents 与 UnifiedEventsRange 两个入口共用一句，避免两处各写一份而漂移。
// 文案里的包名口径随 M3 的 `sessionstore` → `store` 改名同步（D-M3-12 / P9）。
var ErrUnifiedEventsRouterMissing = errors.New("unified events: store router is not attached")

// SummaryLog 是 B 类（llm/tool 意图-效果）脱敏摘要的统一日志：
// 内存 append-only + 可选持久化钩子，形态镜像 plan.EventSink。
// 持久化把摘要转换为 frameworkevent.Event（Source=seelex.telemetry.summary），
// 经 Runtime.SetEventPersister 与 A 类事实写入同一 store 事件库。
// 节点级定位靠 Scope.NodeID（节点会话事件按主会话 session_id 落库，
// 与框架 runner 事件的短期桥约定一致，见 backend/engine/assemble/events.go）。
type SummaryLog struct {
	mu        sync.Mutex
	st        []seeletelemetry.SummaryEvent
	persister func(context.Context, frameworkevent.Event) error
}

// NewSummaryLog 创建摘要统一日志。
func NewSummaryLog() *SummaryLog {
	return &SummaryLog{st: make([]seeletelemetry.SummaryEvent, 0)}
}

// RecordSummary 实现 seeletelemetry.SummaryRecorder（best-effort，void）。
func (log *SummaryLog) RecordSummary(event seeletelemetry.SummaryEvent) {
	_ = log.Append(context.Background(), event)
}

// Append 追加摘要到内存日志并调用可选持久化钩子（与 A 类事实同库）。
func (log *SummaryLog) Append(ctx context.Context, event seeletelemetry.SummaryEvent) error {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	log.st = append(log.st, event)
	persister := log.persister
	log.mu.Unlock()
	if persister != nil {
		return persister(ctx, summaryEventToFrameworkEvent(event))
	}
	return nil
}

// Summaries 返回内存日志的只读快照（进程存活时的实时摘要）。
func (log *SummaryLog) Summaries() []seeletelemetry.SummaryEvent {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]seeletelemetry.SummaryEvent(nil), log.st...)
}

// SetPersister 安装摘要持久化钩子（与 A 类事实同一 store 事件库）。
func (log *SummaryLog) SetPersister(fn func(context.Context, frameworkevent.Event) error) {
	if log == nil {
		return
	}
	log.mu.Lock()
	log.persister = fn
	log.mu.Unlock()
}

// summaryEventToFrameworkEvent 把脱敏摘要转换为统一事件库的框架事件形态。
// Sequence 用 At.UnixNano()（与 plan.AppendPhase 同策略），保证同库去重
// 合并（mergeEventLogEntries 按 Seq 幂等）不会把多次摘要折叠成一条。
func summaryEventToFrameworkEvent(event seeletelemetry.SummaryEvent) frameworkevent.Event {
	status := frameworkevent.StatusCompleted
	if event.Status == "failed" {
		status = frameworkevent.StatusFailed
	}
	content, _ := json.Marshal(event)
	return frameworkevent.Event{
		Sequence:   uint64(event.At.UnixNano()),
		OccurredAt: event.At,
		Source:     SummaryEventSource,
		Type:       frameworkevent.TypeLifecycle,
		Status:     status,
		Scope:      frameworkevent.Scope{NodeID: event.NodeID},
		Content:    content,
	}
}

// UnifiedEventView 是统一事件查询的返回视图：
// Events 为持久统一日志（A 类事实 + B 类摘要，同库合一，按追加顺序），
// Live 为 B 类实时遥测（进程存活时的 tracer 视图）。
type UnifiedEventView struct {
	Events []frameworkevent.Event
	Live   frameworktelemetry.ViewModel
}

// UnifiedEventReader 是统一事件查询门面：单一追加日志源的持久读取
// （Load）+ B 类实时遥测（Live）。查询按 sessionID 关联两轨，nodeID
// 非空时过滤持久日志（Scope.NodeID）；调用方持引用按需回源，不复制 payload。
type UnifiedEventReader struct {
	// Load 全量读（legacy；新代码优先 QueryRange，按需区间读 + 实时轨合并）。
	Load func(ctx context.Context, sessionID string) ([]frameworkevent.Event, error)
	// LoadRange 按 Seq 区间（含端点）读回事实轨；(0,0) = 全量。
	LoadRange func(ctx context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error)
	Live      func(ctx context.Context, limit int) (frameworktelemetry.ViewModel, error)
}

// UnifiedEventsDeps 是按调用装配的宿主面（每次查询取一次读数，保持"Router 可能后装配"的语义）。
type UnifiedEventsDeps struct {
	// Router 取 store 事件库路由；nil 返回值 → `ErrUnifiedEventsRouterMissing`。
	Router func() *store.Router
	// Live 取 B 类实时遥测（宿主封装 tracer 查询与"未装配 tracer"的报错）。可为 nil。
	Live func(ctx context.Context, limit int) (frameworktelemetry.ViewModel, error)
}

// UnifiedEvents 是统一事件查询的域类型：`Query` / `QueryRange` 两个读法共用同一份装配规则。
type UnifiedEvents struct {
	deps UnifiedEventsDeps
}

// NewUnifiedEvents 装配一例（装配点是组合根；也可按调用临时装配，语义相同）。
func NewUnifiedEvents(deps UnifiedEventsDeps) *UnifiedEvents {
	return &UnifiedEvents{deps: deps}
}

// Query 按 sessionID/nodeID 查询统一事件视图（A 类事实 + B 类摘要 + B 类实时遥测）。
func (events *UnifiedEvents) Query(ctx context.Context, sessionID, nodeID string, limit int) (UnifiedEventView, error) {
	router := events.router()
	if router == nil {
		return UnifiedEventView{}, ErrUnifiedEventsRouterMissing
	}
	eventStore := store.NewEventStore(router)
	reader := &UnifiedEventReader{
		Load:      eventStore.Load,
		LoadRange: eventStore.LoadRange,
		Live:      events.deps.Live,
	}
	return reader.Query(ctx, sessionID, nodeID, limit)
}

// QueryRange 按 Seq 区间查询统一事件视图（事实轨 LoadRange + 实时轨合并）。
func (events *UnifiedEvents) QueryRange(ctx context.Context, sessionID, nodeID string, fromSeq, toSeq uint64, limit int) (UnifiedEventView, error) {
	router := events.router()
	if router == nil {
		return UnifiedEventView{}, ErrUnifiedEventsRouterMissing
	}
	eventStore := store.NewEventStore(router)
	reader := &UnifiedEventReader{
		LoadRange: eventStore.LoadRange,
		Live:      events.deps.Live,
	}
	return reader.QueryRange(ctx, sessionID, nodeID, fromSeq, toSeq, limit)
}

// router 取本次查询的事件库路由（deps 未装配或 Router 未接线 → nil）。
func (events *UnifiedEvents) router() *store.Router {
	if events == nil || events.deps.Router == nil {
		return nil
	}
	return events.deps.Router()
}

// Query 按 sessionID/nodeID 返回统一事件视图。
func (reader *UnifiedEventReader) Query(ctx context.Context, sessionID, nodeID string, limit int) (UnifiedEventView, error) {
	return reader.query(ctx, sessionID, nodeID, limit, func(ctx context.Context, sessionID string) ([]frameworkevent.Event, error) {
		if reader == nil || reader.Load == nil {
			return nil, fmt.Errorf("unified events: reader is unavailable")
		}
		return reader.Load(ctx, sessionID)
	})
}

// QueryRange 按 Seq 区间返回统一事件视图（G7 收口：事实轨从"整段 Load"演进
// 为按需 LoadRange，实时轨照旧合并；fromSeq/toSeq 均 0 = 全量）。
func (reader *UnifiedEventReader) QueryRange(ctx context.Context, sessionID, nodeID string, fromSeq, toSeq uint64, limit int) (UnifiedEventView, error) {
	return reader.query(ctx, sessionID, nodeID, limit, func(ctx context.Context, sessionID string) ([]frameworkevent.Event, error) {
		if reader.LoadRange == nil {
			return nil, fmt.Errorf("unified events: range reader is unavailable")
		}
		return reader.LoadRange(ctx, sessionID, fromSeq, toSeq)
	})
}

func (reader *UnifiedEventReader) query(
	ctx context.Context,
	sessionID, nodeID string,
	limit int,
	load func(context.Context, string) ([]frameworkevent.Event, error),
) (UnifiedEventView, error) {
	var view UnifiedEventView
	if reader == nil || load == nil {
		return view, fmt.Errorf("unified events: reader is unavailable")
	}
	events, err := load(ctx, sessionID)
	if err != nil {
		return view, err
	}
	view.Events = filterUnifiedEvents(events, nodeID, limit)
	if reader.Live != nil {
		view.Live, err = reader.Live(ctx, limit)
		if err != nil {
			return view, err
		}
	}
	return view, nil
}

// UnifiedEventTopic 把框架执行事实事件映射为 middle/protocol/event 桥接的
// (channel, sid) 契约（G2 白名单镜像）：事件带 agent.runtime 位置的
// session_id → 会话类事件，sid 必填；无会话位置的进程级事件 → sid 空。
// 本函数只给出映射（纯函数），实际投递由装配层 adapter 完成——引擎侧在依赖方向下层，
// 不反向 import middle/protocol/event。
func UnifiedEventTopic(event frameworkevent.Event) (channel string, sessionID string, sessionScoped bool) {
	for _, location := range event.Locations {
		if location.Kind != "agent.runtime" {
			continue
		}
		if sid := location.IDs["session_id"]; sid != "" {
			return string(event.Type), sid, true
		}
	}
	return string(event.Type), "", false
}

// filterUnifiedEvents 按 nodeID 过滤并按 limit 截断（不修改入参切片）。
func filterUnifiedEvents(events []frameworkevent.Event, nodeID string, limit int) []frameworkevent.Event {
	filtered := make([]frameworkevent.Event, 0, len(events))
	for _, event := range events {
		if nodeID != "" && event.Scope.NodeID != nodeID {
			continue
		}
		filtered = append(filtered, event)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}
