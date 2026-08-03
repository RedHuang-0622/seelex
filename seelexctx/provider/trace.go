package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// TraceSource 是遥测追踪的最小读取面（*telemetry.MemoryTracer 满足）。
// slice 8：TraceProvider 不再依赖旧 seelectx/tracer 树，改读 telemetry
// 生命周期事件（llm/tool intent-effect）投影。
type TraceSource interface {
	Query(ctx context.Context, query telemetry.Query) (telemetry.ViewModel, error)
}

// TraceProvider 实现 Provider 接口，从 telemetry 生命周期事件提取结构信息。
type TraceProvider struct {
	src       TraceSource
	goal      string
	sessionID string
}

// NewTraceProvider 构造基于 telemetry 追踪的导出器。
func NewTraceProvider(src TraceSource, sessionID string) *TraceProvider {
	if src == nil {
		panic("provider: TraceProvider requires non-nil trace source")
	}
	return &TraceProvider{src: src, sessionID: sessionID}
}

// NewTraceProviderWithGoal 构造并显式设置目标。
func NewTraceProviderWithGoal(src TraceSource, sessionID, goal string) *TraceProvider {
	if src == nil {
		panic("provider: TraceProvider requires non-nil trace source")
	}
	return &TraceProvider{src: src, goal: goal, sessionID: sessionID}
}

func (p *TraceProvider) Name() string { return "trace" }

// Export 从 telemetry 生命周期事件（llm/tool intent-effect）提取结构信息。
// 事件按 gen_ai.agent.id（会话 ID）过滤：llm/tool 事件由会话循环写入时
// 携带 AttributeGenAIAgentID = sessionID（Seele session/loop.go）。不过滤
// 会取到进程级全局遥测（并行子代理/其他会话事件串扰——父证据与 merge-back
// 的 Findings/Decisions 互相累积，审查 #3）。
func (p *TraceProvider) Export(ctx context.Context) (*snapshot.ContextSnapshot, error) {
	query := telemetry.Query{
		Limit: 200,
		Attributes: map[string]string{
			telemetry.AttributeGenAIAgentID: p.sessionID,
		},
	}
	view, err := p.src.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("provider: query telemetry: %w", err)
	}
	snap := &snapshot.ContextSnapshot{
		SourceSessionID: p.sessionID,
		ExportedAt:      time.Now(),
		Goal:            p.goal,
	}
	if len(view.Events) == 0 {
		return snap, nil
	}
	snap.Decisions = make([]snapshot.Decision, 0, 8)
	snap.Findings = make([]string, 0, 8)

	for _, event := range view.Events {
		switch event.Type {
		case telemetry.EventLLMAfter:
			extractLLMInfo(event, snap)
		case telemetry.EventToolAfter:
			extractToolDecision(event, snap)
		}
	}

	if len(snap.Decisions) == 0 {
		snap.Decisions = nil
	}
	if len(snap.Findings) == 0 {
		snap.Findings = nil
	}
	return snap, nil
}

// ── llm 事件提取 ───────────────────────────────────────────────

// extractLLMInfo 从 llm.after 事件提取 token 用量与模型信息
// （gen_ai.usage.* 语义属性，见 Seele telemetry semconv）。
func extractLLMInfo(event telemetry.Event, snap *snapshot.ContextSnapshot) {
	tokens := attrInt(event.Attributes, telemetry.AttributeGenAIUsageInput) +
		attrInt(event.Attributes, telemetry.AttributeGenAIUsageOutput)
	if tokens > 0 {
		snap.TokenEstimate += tokens
	}
	if model := attrString(event.Attributes, telemetry.AttributeGenAIRequestModel); model != "" {
		snap.Findings = append(snap.Findings,
			fmt.Sprintf("追踪 %s: 使用模型 %s", event.TraceID, model))
	}
	if event.Status == telemetry.StatusError {
		snap.Findings = append(snap.Findings,
			fmt.Sprintf("LLM 在 %s 中返回错误", event.Name))
	}
}

// ── tool 事件提取 ──────────────────────────────────────────────

// extractToolDecision 从 tool.after 事件提取工具决策
// （gen_ai.tool.name 语义属性）。
func extractToolDecision(event telemetry.Event, snap *snapshot.ContextSnapshot) {
	toolName := attrString(event.Attributes, telemetry.AttributeGenAIToolName)
	if toolName == "" {
		toolName = event.Name
	}
	if toolName == "" {
		return
	}
	dec := snapshot.Decision{
		What: fmt.Sprintf("调用工具 %s", toolName),
		Why:  fmt.Sprintf("在 %s 中执行", event.Name),
	}
	if event.Status == telemetry.StatusError && event.Error != nil {
		dec.Why = fmt.Sprintf("执行 %s 时出错: %s", event.Name, event.Error.Message)
	}
	snap.Decisions = append(snap.Decisions, dec)
}

// ── 属性读取辅助 ────────────────────────────────────────────────

func attrString(attributes telemetry.Attributes, key string) string {
	if attributes == nil {
		return ""
	}
	value, ok := attributes[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func attrInt(attributes telemetry.Attributes, key string) int {
	text := attrString(attributes, key)
	if text == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(text, "%d", &n); err != nil {
		return 0
	}
	return n
}
