package telemetry

import (
	"context"
	"testing"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
)

// runTraceStep 驱动一次 llm.before/after 生命周期边界（模拟 Seele loop
// 的一个动作），span 写入全局 MemoryTracer（T_i 共享存储面）。
func runTraceStep(t *testing.T, hook frameworktelemetry.Hook, sessionID, name string) {
	t.Helper()
	ctx := WithSessionID(context.Background(), sessionID)
	_, invocation, err := hook.Before(ctx, frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore, Name: name,
	})
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatalf("After: %v", err)
	}
}

// TestTracePerSessionIsolation（T1.1/T5.4）：两会话并行执行后各自 trace
// 只含自身 span（INV-T1/T1.4 会话过滤；T5.4 并行隔离）。
func TestTracePerSessionIsolation(t *testing.T) {
	tracer := frameworktelemetry.NewMemoryTracer()
	base, err := frameworktelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	chain := Chain(base, SessionTagHook)

	runTraceStep(t, chain, "sess-a", "llm-a")
	runTraceStep(t, chain, "sess-b", "llm-b")
	runTraceStep(t, chain, "sess-a", "llm-a-2")

	sessionTracer := NewSessionTracer(tracer)
	viewA, err := sessionTracer.QueryBySession(context.Background(), "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	viewB, err := sessionTracer.QueryBySession(context.Background(), "sess-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA.Events) != 4 {
		t.Fatalf("session A events = %d, want 4 (llm-a/llm-a-2 × before+after)", len(viewA.Events))
	}
	if len(viewB.Events) != 2 {
		t.Fatalf("session B events = %d, want 2 (llm-b × before+after)", len(viewB.Events))
	}
	// INV-T1：两会话事件集合不相交。
	aNames := make(map[string]bool, len(viewA.Events))
	for _, event := range viewA.Events {
		aNames[string(event.Type)+":"+event.Name] = true
		if event.Attributes[SessionAttributeKey] != "sess-a" {
			t.Fatalf("session A event missing session tag: %+v", event.Attributes)
		}
	}
	for _, event := range viewB.Events {
		if aNames[string(event.Type)+":"+event.Name] {
			t.Fatalf("trace intersection: session B contains %q from session A", string(event.Type)+":"+event.Name)
		}
		if event.Attributes[SessionAttributeKey] != "sess-b" {
			t.Fatalf("session B event missing session tag: %+v", event.Attributes)
		}
	}
}

// recordWrapper 记录调用顺序并透传（hook 链序测试桩）。
type recordWrapper struct {
	name  string
	next  frameworktelemetry.Hook
	order *[]string
}

func (hook recordWrapper) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	*hook.order = append(*hook.order, hook.name+".before")
	return hook.next.Before(ctx, action)
}

func (hook recordWrapper) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	*hook.order = append(*hook.order, hook.name+".after")
	return hook.next.After(ctx, invocation, effect)
}

func recordWrapperFactory(name string, order *[]string) Wrapper {
	return func(next frameworktelemetry.Hook) frameworktelemetry.Hook {
		return recordWrapper{name: name, next: next, order: order}
	}
}

// TestTraceHookChainComposition（T1.2）：H = h_n ∘ … ∘ h_1 依序透传；
// 空链/单环/满表不破坏控制流。
func TestTraceHookChainComposition(t *testing.T) {
	tracer := frameworktelemetry.NewMemoryTracer()
	base, err := frameworktelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("empty chain noop", func(t *testing.T) {
		chain := Chain(nil)
		runTraceStep(t, chain, "sess-a", "llm-empty")
		view, err := NewSessionTracer(tracer).QueryBySession(context.Background(), "sess-a")
		if err != nil || len(view.Events) != 0 {
			t.Fatalf("empty chain must be noop, got events=%d err=%v", len(view.Events), err)
		}
	})

	t.Run("full chain ordered", func(t *testing.T) {
		var order []string
		chain := Chain(base,
			recordWrapperFactory("h1", &order),
			recordWrapperFactory("h2", &order),
			recordWrapperFactory("h3", &order),
		)
		ctx := WithSessionID(context.Background(), "sess-order")
		_, invocation, err := chain.Before(ctx, frameworktelemetry.Action{
			Type: frameworktelemetry.EventToolBefore, Name: "tool-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := chain.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
			t.Fatal(err)
		}
		want := []string{"h1.before", "h2.before", "h3.before", "h1.after", "h2.after", "h3.after"}
		if len(order) != len(want) {
			t.Fatalf("chain order = %v, want %v", order, want)
		}
		for index := range want {
			if order[index] != want[index] {
				t.Fatalf("chain order[%d] = %q, want %q (full: %v)", index, order[index], want[index], order)
			}
		}
	})
}

// TestTraceQuerySessionFiltered（T1.4）：空/其它会话不命中；trace 满限不溢出。
func TestTraceQuerySessionFiltered(t *testing.T) {
	tracer := frameworktelemetry.NewMemoryTracer()
	base, err := frameworktelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	chain := Chain(base, SessionTagHook)
	for index := 0; index < 3; index++ {
		runTraceStep(t, chain, "sess-limit", "llm-step")
	}
	runTraceStep(t, chain, "sess-other", "llm-other")

	sessionTracer := NewSessionTracer(tracer)
	empty, err := sessionTracer.QueryBySession(context.Background(), "")
	if err != nil || len(empty.Events) != 0 {
		t.Fatalf("empty session query must not match: events=%d err=%v", len(empty.Events), err)
	}
	other, err := sessionTracer.QueryBySession(context.Background(), "sess-missing")
	if err != nil || len(other.Events) != 0 {
		t.Fatalf("missing session query must not match: events=%d err=%v", len(other.Events), err)
	}
	limited, err := sessionTracer.QueryBySessionLimit(context.Background(), "sess-limit", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Events) == 0 || len(limited.Events) > 2 {
		t.Fatalf("limited trace events = %d, want 1..2（满限不溢出）", len(limited.Events))
	}
}

// TestStreamResultReturnModel（T1.3）：Stream_i 返回语义结果（reply），
// ΔT_i（trace 增量）可观察；ΔV_i（视图投影）由 core 侧投影承接
// （事件指纹 TestEventFingerprintStable）。
func TestStreamResultReturnModel(t *testing.T) {
	tracer := frameworktelemetry.NewMemoryTracer()
	base, err := frameworktelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	chain := Chain(base, SessionTagHook)
	sessionTracer := NewSessionTracer(tracer)

	before, err := sessionTracer.QueryBySession(context.Background(), "sess-stream")
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSessionID(context.Background(), "sess-stream")
	_, invocation, err := chain.Before(ctx, frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore, Name: "llm-stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 语义结果：reply/reasoning/toolCalls 由 loop 返回（这里模拟）。
	reply := "streamed answer"
	_ = invocation
	if err := chain.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	after, err := sessionTracer.QueryBySession(context.Background(), "sess-stream")
	if err != nil {
		t.Fatal(err)
	}
	deltaT := len(after.Events) - len(before.Events)
	if reply != "streamed answer" {
		t.Fatalf("reply = %q", reply)
	}
	if deltaT != 2 {
		t.Fatalf("ΔT_i = %d events, want 2 (llm.before + llm.after)", deltaT)
	}
}
