package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/telemetry"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// ── 遥测装配（slice 8：seelectx/tracer → telemetry） ────────────

func TestNewTracerAndLifecycleHookWireLLMIntentEffect(t *testing.T) {
	tracer := seeletelemetry.NewTracer()
	if tracer == nil {
		t.Fatal("seeletelemetry.NewTracer returned nil")
	}
	hook, err := seeletelemetry.NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	if hook == nil {
		t.Fatal("seeletelemetry.NewLifecycleHook returned nil hook")
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
	for _, event := range view.Events {
		counts[event.Type]++
	}
	for _, eventType := range []telemetry.EventType{
		telemetry.EventLLMBefore, telemetry.EventLLMAfter,
		telemetry.EventToolBefore, telemetry.EventToolAfter,
	} {
		if counts[eventType] == 0 {
			t.Fatalf("missing lifecycle event %q: counts=%#v\n%s", eventType, counts, dumpLifecycleEvents(view))
		}
	}

	// intent-effect 配对：**按 operation 读**，不按"视图里最后一个"读。
	//
	// 为什么必须这样读（2026-09-26 的偶发红实测）：`MemoryTracer.Query` 遍历
	// `trace.spans`（map，迭代顺序随机）后只按 Timestamp 做**稳定**排序，而 Windows
	// 的时钟粒度粗（~0.5ms）——末轮 llm 的 before/after 常落在同一时间戳里，同分事件
	// 因此保持"随机 map 顺序"。旧写法"取最后一个 before / 最后一个 after 比 correlation"
	// 会跨两个 operation 读，负载下必然偶发失配（实测同一份事件集连查 200 次得到 4 种
	// 顺序）。`trace.Operations` 是框架按 CorrelationID 归并出的 intent/effect 投影，
	// 与视图顺序无关——它才是这条判据的正确读面。
	paired := map[telemetry.EventType]bool{}
	for _, trace := range view.Traces {
		for _, operation := range trace.Operations {
			intent, effect := operation.Intent, operation.Effect
			if intent == nil || effect == nil {
				continue
			}
			if intent.CorrelationID == "" || intent.CorrelationID != effect.CorrelationID {
				t.Fatalf("intent-effect correlation mismatch inside one operation: %s/%s corr=%q/%q\n%s",
					intent.Type, effect.Type, intent.CorrelationID, effect.CorrelationID, dumpLifecycleEvents(view))
			}
			switch intent.Type {
			case telemetry.EventLLMBefore:
				if effect.Type == telemetry.EventLLMAfter {
					paired[telemetry.EventLLMBefore] = true
				}
			case telemetry.EventToolBefore:
				if effect.Type == telemetry.EventToolAfter {
					paired[telemetry.EventToolBefore] = true
				}
			}
		}
	}
	for _, eventType := range []telemetry.EventType{telemetry.EventLLMBefore, telemetry.EventToolBefore} {
		if !paired[eventType] {
			t.Fatalf("%s 的 intent-effect 没有成对投影（operations 里找不到完整的一对）\n%s",
				eventType, dumpLifecycleEvents(view))
		}
	}
	if len(view.Traces) == 0 {
		t.Fatal("trace view has no traces after session chat")
	}
}

// dumpLifecycleEvents 把一个 telemetry 视图压成可读的事件清单（顺序敏感：它正是
// 用来在失败时看清"视图顺序"的）。时间戳按 RFC3339Nano 打印，correlation 截断到 8 位。
func dumpLifecycleEvents(view telemetry.ViewModel) string {
	var builder strings.Builder
	builder.WriteString("telemetry 视图事件（视图顺序）：\n")
	for _, event := range view.Events {
		correlation := event.CorrelationID
		if len(correlation) > 8 {
			correlation = correlation[:8]
		}
		fmt.Fprintf(&builder, "  %s %-12s phase=%-6s corr=%-8s trace=%s span=%s status=%s at=%s\n",
			event.Type, "", event.Phase, correlation, event.TraceID, event.SpanID, event.Status,
			event.Timestamp.Format(time.RFC3339Nano))
	}
	builder.WriteString("trace.Operations（按 CorrelationID 归并，顺序无关）：\n")
	for _, trace := range view.Traces {
		for _, operation := range trace.Operations {
			correlation := operation.CorrelationID
			if len(correlation) > 8 {
				correlation = correlation[:8]
			}
			intent, effect := "nil", "nil"
			if operation.Intent != nil {
				intent = string(operation.Intent.Type)
			}
			if operation.Effect != nil {
				effect = string(operation.Effect.Type)
			}
			fmt.Fprintf(&builder, "  corr=%-8s intent=%-12s effect=%-12s status=%s\n",
				correlation, intent, effect, operation.Status)
		}
	}
	return builder.String()
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
