package telemetry

import (
	"context"
	"sync"
	"time"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// summarySlowThreshold 是 B 类摘要的默认慢调用阈值：正常成功但耗时超过
// 该阈值的调用也记为摘要（其余正常成功调用默认丢弃）。
const summarySlowThreshold = 30 * time.Second

// pendingSummaryMax 是 Before/After 配对表中最多同时挂起的条目数。
// 超过上限时新记录直接丢弃（best-effort），避免异常路径无界增长。
const pendingSummaryMax = 4096

// SummaryEvent 是 B 类（llm/tool 意图-效果）的脱敏摘要事件。
// 刻意只含审计所需的最小字段：Kind/Name/Status/DurationMS/At/NodeID。
// 绝不含工具参数、结果、LLM 正文或账号数据（与 DiagnosticHook 的脱敏
// 原则一致，见 docs/2026-08-14-decoupling/06-unified-event-store-decision.md）。
type SummaryEvent struct {
	Kind       string    // "llm" | "tool"
	Name       string    // action.Name（工具名 / 模型名）
	Status     string    // "failed" | "completed"
	DurationMS int64     // 调用耗时（毫秒）
	At         time.Time // 调用开始时间
	NodeID     string    // 空 = 主会话 / 无 NodeScope
}

// SummaryRecorder 是摘要事件的记录面（由 Runtime 注入 SummaryLog 实现；
// 接口化避免 internal/telemetry 反向依赖 seelebridge 根包）。
type SummaryRecorder interface {
	RecordSummary(event SummaryEvent)
}

// SummaryOption 配置 SummaryHook。
type SummaryOption func(*summaryOptions)

type summaryOptions struct {
	now           func() time.Time
	slowThreshold time.Duration
}

// WithNow 注入确定性时钟（测试用）；默认 time.Now。
func WithNow(now func() time.Time) SummaryOption {
	return func(options *summaryOptions) {
		if now != nil {
			options.now = now
		}
	}
}

// WithSlowThreshold 覆盖慢调用判定阈值；默认 summarySlowThreshold。
func WithSlowThreshold(threshold time.Duration) SummaryOption {
	return func(options *summaryOptions) {
		if threshold > 0 {
			options.slowThreshold = threshold
		}
	}
}

// pendingSummary 是 Before/After 之间挂起的配对记录。
type pendingSummary struct {
	kind      string
	name      string
	nodeID    string
	startedAt time.Time
}

// pairKey 用 trace span 唯一标识一次 Before/After 配对
// （SpanID 每次操作唯一；嵌套调用各自独立）。
type pairKey struct {
	traceID string
	spanID  string
}

// SummaryHook 在 telemetry 链上记录 llm/tool 意图-效果的脱敏摘要：
// Before 时按 trace span 记录起点，After 时计算耗时并按策略落摘要
// （失败必记；正常成功但超阈值记；其余成功丢弃）。best-effort：
// recorder 为 nil 安全，绝不改变执行路径。
type SummaryHook struct {
	next     frameworktelemetry.Hook
	recorder SummaryRecorder
	now      func() time.Time
	slow     time.Duration

	mu      sync.Mutex
	pending map[pairKey]pendingSummary
}

// NewSummaryHook 构造摘要观察面（Wrapper 形态，由 Chain 负责透传）；
// next 为 nil 时降级为透传空实现。
func NewSummaryHook(recorder SummaryRecorder, options ...SummaryOption) Wrapper {
	opts := summaryOptions{now: time.Now, slowThreshold: summarySlowThreshold}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return func(next frameworktelemetry.Hook) frameworktelemetry.Hook {
		if next == nil {
			next = noopHook{}
		}
		return &SummaryHook{
			next:     next,
			recorder: recorder,
			now:      opts.now,
			slow:     opts.slowThreshold,
			pending:  make(map[pairKey]pendingSummary),
		}
	}
}

// Before 实现 telemetry.Hook：记录 llm/tool 起点后透传。
func (hook *SummaryHook) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	isLLMOrTool := action.Type == frameworktelemetry.EventLLMBefore || action.Type == frameworktelemetry.EventToolBefore
	var startedAt time.Time
	if isLLMOrTool {
		startedAt = hook.now()
	}
	nextCtx, invocation, err := hook.next.Before(ctx, action)
	if err != nil || hook.recorder == nil || !isLLMOrTool {
		return nextCtx, invocation, err
	}
	hook.remember(action, nextCtx, startedAt)
	return nextCtx, invocation, nil
}

// After 实现 telemetry.Hook：透传并按策略落摘要。
func (hook *SummaryHook) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	err := hook.next.After(ctx, invocation, effect)
	hook.emit(ctx, effect)
	return err
}

// remember 按 trace span 记录配对条目；无 trace 上下文或表满时丢弃。
func (hook *SummaryHook) remember(action frameworktelemetry.Action, ctx context.Context, startedAt time.Time) {
	trace, ok := frameworktelemetry.TraceFromContext(ctx)
	if !ok {
		return
	}
	kind := "tool"
	if action.Type == frameworktelemetry.EventLLMBefore {
		kind = "llm"
	}
	nodeID := ""
	if scope, ok := model.NodeScopeFromContext(ctx); ok {
		nodeID = scope.NodeID
	}
	key := pairKey{traceID: trace.TraceID, spanID: trace.SpanID}
	hook.mu.Lock()
	defer hook.mu.Unlock()
	if len(hook.pending) >= pendingSummaryMax {
		return // best-effort：满表丢弃，避免无界增长
	}
	hook.pending[key] = pendingSummary{
		kind:      kind,
		name:      action.Name,
		nodeID:    nodeID,
		startedAt: startedAt,
	}
}

// emit 计算耗时并按策略落摘要：失败必记；正常成功且未超阈值丢弃。
func (hook *SummaryHook) emit(ctx context.Context, effect frameworktelemetry.Effect) {
	trace, ok := frameworktelemetry.TraceFromContext(ctx)
	if !ok {
		return
	}
	key := pairKey{traceID: trace.TraceID, spanID: trace.SpanID}
	hook.mu.Lock()
	pending, ok := hook.pending[key]
	if ok {
		delete(hook.pending, key)
	}
	hook.mu.Unlock()
	if !ok {
		return
	}
	if hook.recorder == nil {
		return
	}
	duration := hook.now().Sub(pending.startedAt)
	failed := effect.Error != nil || effect.Status == frameworktelemetry.StatusError
	if !failed && duration < hook.slow {
		return
	}
	status := "completed"
	if failed {
		status = "failed"
	}
	hook.recorder.RecordSummary(SummaryEvent{
		Kind:       pending.kind,
		Name:       pending.name,
		Status:     status,
		DurationMS: duration.Milliseconds(),
		At:         pending.startedAt,
		NodeID:     pending.nodeID,
	})
}
