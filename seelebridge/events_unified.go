// 统一事件库：单一追加日志源 + 分层投影（解耦方案 §02.3 / §04.7 的长期形态，
// 见 docs/2026-08-14-decoupling/06-unified-event-store-decision.md）。
//
// 形态：A 类（plan/节点执行事实）全量持久化是唯一事实源；B 类
// （llm/tool 意图-效果遥测）留内存实时面，另以有界脱敏摘要追加进
// 同一 sessionstore 事件库（B 摘要与 A 类同库，查询统一、存储不双写）。
// 统一查询按 sessionID/nodeID 关联两轨，用引用/索引而不是复制 payload。
package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// summaryEventSource 是 B 类摘要事件在统一事件库（sessionstore 事件库）中的
// 稳定 Source 标识，与 A 类事实（workplan.runner / seelex.subagent）同库共存。
const summaryEventSource = "seelex.telemetry.summary"

// SummaryLog 是 B 类（llm/tool 意图-效果）脱敏摘要的统一日志：
// 内存 append-only + 可选持久化钩子，形态镜像 plan.EventSink。
// 持久化把摘要转换为 frameworkevent.Event（Source=seelex.telemetry.summary），
// 经 Runtime.SetEventPersister 与 A 类事实写入同一 sessionstore 事件库。
// 节点级定位靠 Scope.NodeID（节点会话事件按主会话 session_id 落库，
// 与框架 runner 事件的短期桥约定一致，见 events.go）。
type SummaryLog struct {
	mu        sync.Mutex
	store     []seeletelemetry.SummaryEvent
	persister func(context.Context, frameworkevent.Event) error
}

// NewSummaryLog 创建摘要统一日志。
func NewSummaryLog() *SummaryLog {
	return &SummaryLog{store: make([]seeletelemetry.SummaryEvent, 0)}
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
	log.store = append(log.store, event)
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
	return append([]seeletelemetry.SummaryEvent(nil), log.store...)
}

// SetPersister 安装摘要持久化钩子（与 A 类事实同一 sessionstore 事件库）。
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
		Source:     summaryEventSource,
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
	Load func(ctx context.Context, sessionID string) ([]frameworkevent.Event, error)
	Live func(ctx context.Context, limit int) (frameworktelemetry.ViewModel, error)
}

// Query 按 sessionID/nodeID 返回统一事件视图。
func (reader *UnifiedEventReader) Query(ctx context.Context, sessionID, nodeID string, limit int) (UnifiedEventView, error) {
	var view UnifiedEventView
	if reader == nil || reader.Load == nil {
		return view, fmt.Errorf("unified events: reader is unavailable")
	}
	events, err := reader.Load(ctx, sessionID)
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

// UnifiedEvents 按 sessionID/nodeID 查询统一事件视图：A 类事实 + B 类摘要的
// 持久统一日志（sessionstore 事件库）+ B 类实时遥测（内存 tracer）。
// sessionstore Router 未装配时返回可读错误。
func (r *Runtime) UnifiedEvents(ctx context.Context, sessionID, nodeID string, limit int) (UnifiedEventView, error) {
	if r == nil {
		return UnifiedEventView{}, fmt.Errorf("unified events: runtime is nil")
	}
	router := r.bindings.getHistoryRouter()
	if router == nil {
		return UnifiedEventView{}, fmt.Errorf("unified events: sessionstore router is not attached")
	}
	reader := &UnifiedEventReader{
		Load: sessionstore.NewEventStore(router).Load,
		Live: func(ctx context.Context, limit int) (frameworktelemetry.ViewModel, error) {
			if r.tracer == nil {
				return frameworktelemetry.ViewModel{}, fmt.Errorf("unified events: tracer is unavailable")
			}
			return r.tracer.Query(ctx, frameworktelemetry.Query{Limit: limit})
		},
	}
	return reader.Query(ctx, sessionID, nodeID, limit)
}
