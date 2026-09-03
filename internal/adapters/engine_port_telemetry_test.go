package adapters

import (
	"context"
	"testing"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
)

// telemetrySessionAttribute 是 seelebridge/internal/telemetry 的
// SessionAttributeKey（"session_id"）。测试直接打 attribute，等价于
// SessionTagHook 在生产链上的产物（该包为 internal，不允许本包引用）。
const telemetrySessionAttribute = "session_id"

// emitLLMUsage 模拟一次带会话标签的 LLM 调用：before/after intent-effect
// 事件写入 tracer，after 事件携带 usage token（与生产 Seele loop 一致）。
func emitLLMUsage(t *testing.T, tracer *frameworktelemetry.MemoryTracer, sessionID string, input, output int) {
	t.Helper()
	hook, err := frameworktelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, invocation, err := hook.Before(ctx, frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore,
		Name: "completion",
		Attributes: frameworktelemetry.Attributes{
			telemetrySessionAttribute: sessionID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{
		Attributes: frameworktelemetry.Attributes{
			telemetrySessionAttribute:                    sessionID,
			frameworktelemetry.AttributeGenAIUsageInput:  input,
			frameworktelemetry.AttributeGenAIUsageOutput: output,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestEnginePortTokenCountForFiltersBySession（G1/M8）：TokenCountFor 只累加
// 带目标会话 session_id 标签的 LLM usage 事件；TokenCount 保持全局合计语义。
func TestEnginePortTokenCountForFiltersBySession(t *testing.T) {
	tracer := frameworktelemetry.NewMemoryTracer()
	emitLLMUsage(t, tracer, "sess-a", 100, 50)
	emitLLMUsage(t, tracer, "sess-b", 7, 3)
	emitLLMUsage(t, tracer, "sess-a", 20, 30)

	port := NewEnginePort(nil, nil, tracer)
	if got := port.TokenCountFor("sess-a"); got != "200" {
		t.Fatalf("TokenCountFor(sess-a) = %q, want 200", got)
	}
	if got := port.TokenCountFor("sess-b"); got != "10" {
		t.Fatalf("TokenCountFor(sess-b) = %q, want 10", got)
	}
	if got := port.TokenCountFor("sess-missing"); got != "0" {
		t.Fatalf("TokenCountFor(sess-missing) = %q, want 0", got)
	}
	if got := port.TokenCountFor(""); got != "0" {
		t.Fatalf("TokenCountFor(\"\") = %q, want 0", got)
	}
	// 全局合计不变（旧语义：跨会话求和）。
	if got := port.TokenCount(); got != "210" {
		t.Fatalf("TokenCount() = %q, want 210 (global sum)", got)
	}
}

// TestEnginePortTokenCountForWithoutTracer 无 tracer（单会话桩）时 per-session
// 计数回退 0，不 panic。
func TestEnginePortTokenCountForWithoutTracer(t *testing.T) {
	port := NewEnginePort(nil, nil, nil)
	if got := port.TokenCountFor("sess-a"); got != "0" {
		t.Fatalf("TokenCountFor without tracer = %q, want 0", got)
	}
}
