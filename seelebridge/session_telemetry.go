package seelebridge

import (
	"context"
	"strconv"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// WithTelemetrySessionID 把会话 ID 注入 ctx（G1/M8 会话入口接线）。
// SessionTagHook 已挂在生产 telemetry hook 链最外层（runtime.go），但此前
// 无人把 runChat 的会话 ID 转写为 telemetry 路由键——本函数经 EnginePort
// 的 ChatStreamFor/ChatStream 调用，使 llm/tool intent-effect 事件都带上
// session_id，per-session trace 查询在生产侧成立（INV-T1/T2）。
func WithTelemetrySessionID(ctx context.Context, sessionID string) context.Context {
	return seetelemetry.WithSessionID(ctx, sessionID)
}

// SessionTokenCount 返回指定会话的 LLM usage token 总数（INV-T2 查询面）：
// 经 SessionTracer 按 session_id attribute 精确过滤，不混入其它会话事件；
// 空会话 ID / 无 tracer / 查询失败一律返回 0（事件尚未发生也算 0）。
func SessionTokenCount(tracer *frameworktelemetry.MemoryTracer, sessionID string) int {
	if tracer == nil || sessionID == "" {
		return 0
	}
	view, err := seetelemetry.NewSessionTracer(tracer).QueryBySessionLimit(context.Background(), sessionID, 200)
	if err != nil {
		return 0
	}
	total := 0
	for _, event := range view.Events {
		if event.Type != frameworktelemetry.EventLLMAfter {
			continue
		}
		total += telemetryAttributeInt(event.Attributes, frameworktelemetry.AttributeGenAIUsageInput)
		total += telemetryAttributeInt(event.Attributes, frameworktelemetry.AttributeGenAIUsageOutput)
	}
	return total
}

// telemetryAttributeInt 把 telemetry attribute 的常见数值形态还原为 int
// （与 adapters.EnginePort.attrTelemetryInt 同款容错；字符串解析失败按 0）。
func telemetryAttributeInt(attributes frameworktelemetry.Attributes, key string) int {
	if attributes == nil {
		return 0
	}
	value, ok := attributes[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, err := strconv.Atoi(typed)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
