package seelebridge_test

import (
	"context"
	"testing"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// captureReactorEngine 记录 ChatStream 收到的 ctx，用于断言 EnginePort 在
// 会话入口注入了 telemetry 会话路由键。
type captureReactorEngine struct {
	sessionID string
	gotCtx    context.Context
}

func (engine *captureReactorEngine) ChatStream(ctx context.Context, _ string, _ func(string)) (string, error) {
	engine.gotCtx = ctx
	return "", nil
}
func (*captureReactorEngine) History() []types.Message    { return nil }
func (*captureReactorEngine) ClearHistory()               {}
func (engine *captureReactorEngine) SessionID() string    { return engine.sessionID }
func (*captureReactorEngine) SetSystemPrompt(string)      {}
func (*captureReactorEngine) SetMaxLoops(int)             {}
func (*captureReactorEngine) AppendHistory(types.Message) {}

// TestEnginePortInjectsTelemetrySessionID（G1/M8 回归）：EnginePort 的会话
// 入口必须把显式 sessionID 转写为 telemetry ctx 键——SessionTagHook 依赖它
// 给 llm/tool intent-effect 事件打会话标签（生产侧此前无人注入）。
func TestEnginePortInjectsTelemetrySessionID(t *testing.T) {
	engine := &captureReactorEngine{sessionID: "sess-tag"}
	port := adapters.NewEnginePort(engine, nil, nil)

	if _, err := port.ChatStreamFor("sess-tag", context.Background(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if got := seetelemetry.SessionIDFromContext(engine.gotCtx); got != "sess-tag" {
		t.Fatalf("ChatStreamFor ctx session = %q, want sess-tag", got)
	}

	// legacy 活跃别名路径同样注入。
	engine.gotCtx = nil
	if _, err := port.ChatStream(context.Background(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if got := seetelemetry.SessionIDFromContext(engine.gotCtx); got != "sess-tag" {
		t.Fatalf("ChatStream ctx session = %q, want sess-tag", got)
	}
}

// emitTaggedLLMUsage 经完整生产链（LifecycleHook + SessionTagHook）产生一次
// 带会话标签的 LLM usage 事件。
func emitTaggedLLMUsage(t *testing.T, hook frameworktelemetry.Hook, sessionID string, input, output int) {
	t.Helper()
	ctx := seetelemetry.WithSessionID(context.Background(), sessionID)
	_, invocation, err := hook.Before(ctx, frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore, Name: "completion",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{
		Attributes: frameworktelemetry.Attributes{
			frameworktelemetry.AttributeGenAIUsageInput:  input,
			frameworktelemetry.AttributeGenAIUsageOutput: output,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSessionTokenCountUsesSessionTaggedEvents（INV-T1/T2 生产面）：公共
// 包装 WithTelemetrySessionID + SessionTokenCount 与内部 SessionTagHook
// 链路一致，两会话 usage 互不串扰。
func TestSessionTokenCountUsesSessionTaggedEvents(t *testing.T) {
	tracer := frameworktelemetry.NewMemoryTracer()
	base, err := frameworktelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	chain := seetelemetry.Chain(base, seetelemetry.SessionTagHook)

	emitTaggedLLMUsage(t, chain, "sess-a", 100, 50)
	emitTaggedLLMUsage(t, chain, "sess-b", 7, 3)
	emitTaggedLLMUsage(t, chain, "sess-a", 20, 30)

	if got := seelebridge.SessionTokenCount(tracer, "sess-a"); got != 200 {
		t.Fatalf("SessionTokenCount(sess-a) = %d, want 200", got)
	}
	if got := seelebridge.SessionTokenCount(tracer, "sess-b"); got != 10 {
		t.Fatalf("SessionTokenCount(sess-b) = %d, want 10", got)
	}
	if got := seelebridge.SessionTokenCount(tracer, "sess-other"); got != 0 {
		t.Fatalf("SessionTokenCount(sess-other) = %d, want 0", got)
	}
	if got := seelebridge.SessionTokenCount(tracer, ""); got != 0 {
		t.Fatalf("SessionTokenCount(\"\") = %d, want 0", got)
	}
}
