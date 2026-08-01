package seelebridge

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/telemetry"
)

// ── 遥测装配（slice 8：seelectx/tracer → telemetry） ────────────

func TestNewTracerAndLifecycleHookWireLLMIntentEffect(t *testing.T) {
	tracer := NewTracer()
	if tracer == nil {
		t.Fatal("NewTracer returned nil")
	}
	hook, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	if hook == nil {
		t.Fatal("NewLifecycleHook returned nil hook")
	}

	// llm intent-effect：Before（意图）→ After（效果），correlation 配对。
	ctx, invocation, err := hook.Before(context.Background(), telemetry.Action{
		Type: telemetry.EventLLMBefore, Name: "completion", SpanName: "llm.completion",
		SpanKind: telemetry.SpanLLM,
		Attributes: telemetry.Attributes{
			telemetry.AttributeGenAIRequestModel: "test-model",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.After(ctx, invocation, telemetry.Effect{
		Attributes: telemetry.Attributes{
			telemetry.AttributeGenAIUsageInput:  100,
			telemetry.AttributeGenAIUsageOutput: 50,
		},
	}); err != nil {
		t.Fatal(err)
	}

	view, err := tracer.Query(context.Background(), telemetry.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var before, after *telemetry.Event
	for index := range view.Events {
		event := view.Events[index]
		if event.Type == telemetry.EventLLMBefore {
			before = &event
		}
		if event.Type == telemetry.EventLLMAfter {
			after = &event
		}
	}
	if before == nil || after == nil {
		t.Fatalf("missing llm.before/llm.after events: %#v", view.Events)
	}
	if before.CorrelationID == "" || before.CorrelationID != after.CorrelationID {
		t.Fatalf("intent-effect correlation mismatch: before=%q after=%q",
			before.CorrelationID, after.CorrelationID)
	}
	if len(view.Traces) == 0 {
		t.Fatal("trace view has no traces")
	}
	// Operations 是 intent-effect 配对投影（trace 视图 API）。
	found := false
	for _, trace := range view.Traces {
		for _, operation := range trace.Operations {
			if operation.Intent != nil && operation.Effect != nil &&
				operation.Intent.Type == telemetry.EventLLMBefore &&
				operation.Effect.Type == telemetry.EventLLMAfter {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no correlated llm intent-effect operation in trace view: %#v", view.Traces)
	}
}

// TestSessionLifecycleEventsLLMAndToolIntentEffect 验证主会话经
// telemetry hook 产生 llm/tool intent-effect 事件（脚本式 completer，
// 无网络）：首轮 LLM 返回 tool_calls → 工具调度 → 次轮文本回复。
func TestSessionLifecycleEventsLLMAndToolIntentEffect(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	// 确定性 completer：首轮调用 echo 工具，次轮返回最终文本。
	scripted := newScriptedNodeCompleter("done")
	scripted.probeTool = "echo"
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"agent-1": scripted})

	runtime.RegisterTool("echo", "回显输入",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(context.Context, string) (string, error) { return `"ok"`, nil },
	)

	sess, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := sess.Chat(context.Background(), "run lifecycle")
	if err != nil {
		t.Fatalf("session chat failed: %v", err)
	}
	if !strings.Contains(reply, "done") {
		t.Fatalf("reply = %q", reply)
	}

	view, err := runtime.Tracer().Query(context.Background(), telemetry.Query{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[telemetry.EventType]int{}
	correlations := map[telemetry.EventType]string{}
	for _, event := range view.Events {
		counts[event.Type]++
		correlations[event.Type] = event.CorrelationID
	}
	for _, eventType := range []telemetry.EventType{
		telemetry.EventLLMBefore, telemetry.EventLLMAfter,
		telemetry.EventToolBefore, telemetry.EventToolAfter,
	} {
		if counts[eventType] == 0 {
			t.Fatalf("missing lifecycle event %q: counts=%#v", eventType, counts)
		}
	}
	// intent-effect 配对：before/after 的 correlation ID 一致。
	for _, pair := range [][2]telemetry.EventType{
		{telemetry.EventLLMBefore, telemetry.EventLLMAfter},
		{telemetry.EventToolBefore, telemetry.EventToolAfter},
	} {
		if correlations[pair[0]] == "" || correlations[pair[0]] != correlations[pair[1]] {
			t.Fatalf("intent-effect correlation mismatch for %s/%s: %#v",
				pair[0], pair[1], correlations)
		}
	}
	if len(view.Traces) == 0 {
		t.Fatal("trace view has no traces after session chat")
	}
}

// TestRuntimeTracerSurvivesSessionRecreation 验证同一 Runtime 的 tracer
// 跨会话保持（trace 视图 API 稳定，会话重建不影响查询源）。
func TestRuntimeTracerSurvivesSessionRecreation(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	first, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected independent sessions")
	}
	if runtime.Tracer() == nil {
		t.Fatal("runtime tracer is nil")
	}
	if _, err := runtime.Tracer().Query(context.Background(), telemetry.Query{}); err != nil {
		t.Fatalf("trace view query failed: %v", err)
	}
}
