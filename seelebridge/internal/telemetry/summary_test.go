package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// summaryRecorderFunc 把回调适配为 SummaryRecorder。
type summaryRecorderFunc func(event SummaryEvent)

func (fn summaryRecorderFunc) RecordSummary(event SummaryEvent) {
	if fn != nil {
		fn(event)
	}
}

// newSummaryTestHook 构造基于真实 LifecycleHook 的摘要钩子（确定性时钟）。
func newSummaryTestHook(t *testing.T, at *time.Time) (*SummaryHook, *frameworktelemetry.MemoryTracer) {
	t.Helper()
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(nil, WithNow(func() time.Time { return *at }))(base)
	return hook.(*SummaryHook), tracer
}

// TestSummaryHookRecordsFailure 验证失败调用必记摘要（Status=failed）。
func TestSummaryHookRecordsFailure(t *testing.T) {
	var got []SummaryEvent
	at := time.Unix(1000, 0)
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(summaryRecorderFunc(func(event SummaryEvent) { got = append(got, event) }),
		WithNow(func() time.Time { return at }))(base)
	ctx, invocation, err := hook.Before(context.Background(), frameworktelemetry.Action{
		Type: frameworktelemetry.EventToolBefore,
		Name: "bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(2 * time.Second)
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{Error: errors.New("boom")}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("summaries = %#v, want 1", got)
	}
	want := SummaryEvent{Kind: "tool", Name: "bash", Status: "failed", DurationMS: 2000, At: time.Unix(1000, 0)}
	if got[0] != want {
		t.Fatalf("summary = %#v, want %#v", got[0], want)
	}
}

// TestSummaryHookDropsNormalSuccess 验证正常成功且未超阈值的调用不落摘要。
func TestSummaryHookDropsNormalSuccess(t *testing.T) {
	var got []SummaryEvent
	at := time.Unix(1000, 0)
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(summaryRecorderFunc(func(event SummaryEvent) { got = append(got, event) }),
		WithNow(func() time.Time { return at }))(base)
	ctx, invocation, err := hook.Before(context.Background(), frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore,
		Name: "gpt-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("summaries = %#v, want none", got)
	}
}

// TestSummaryHookRecordsSlowSuccess 验证正常成功但超阈值的调用记 completed。
func TestSummaryHookRecordsSlowSuccess(t *testing.T) {
	var got []SummaryEvent
	at := time.Unix(1000, 0)
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(summaryRecorderFunc(func(event SummaryEvent) { got = append(got, event) }),
		WithNow(func() time.Time { return at }),
		WithSlowThreshold(10*time.Second))(base)
	ctx, invocation, err := hook.Before(context.Background(), frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore,
		Name: "gpt-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(31 * time.Second)
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != "completed" || got[0].DurationMS != 31000 {
		t.Fatalf("summaries = %#v, want single completed 31000ms", got)
	}
}

// TestSummaryHookCapturesNodeID 验证 NodeScope 注入后摘要携带 nodeID。
func TestSummaryHookCapturesNodeID(t *testing.T) {
	var got []SummaryEvent
	at := time.Unix(1000, 0)
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(summaryRecorderFunc(func(event SummaryEvent) { got = append(got, event) }),
		WithNow(func() time.Time { return at }))(base)
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "node-42"})
	ctx, invocation, err := hook.Before(ctx, frameworktelemetry.Action{
		Type: frameworktelemetry.EventToolBefore,
		Name: "bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Minute)
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{Error: errors.New("timeout")}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].NodeID != "node-42" {
		t.Fatalf("summaries = %#v, want nodeID node-42", got)
	}
}

// TestSummaryHookCleansPendingAfterAfter 验证 After 后配对条目被清理（不泄漏）。
func TestSummaryHookCleansPendingAfterAfter(t *testing.T) {
	at := time.Unix(1000, 0)
	var got []SummaryEvent
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(summaryRecorderFunc(func(event SummaryEvent) { got = append(got, event) }),
		WithNow(func() time.Time { return at }))(base)
	ctx, invocation, err := hook.Before(context.Background(), frameworktelemetry.Action{
		Type: frameworktelemetry.EventToolBefore,
		Name: "bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending := hook.(*SummaryHook).pending; len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	at = at.Add(2 * time.Second)
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	if pending := hook.(*SummaryHook).pending; len(pending) != 0 {
		t.Fatalf("pending = %d, want 0 after After", len(pending))
	}
}

// TestSummaryHookIgnoresNonLLMToolEvents 验证非 llm/tool 事件不落摘要。
func TestSummaryHookIgnoresNonLLMToolEvents(t *testing.T) {
	var got []SummaryEvent
	at := time.Unix(1000, 0)
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewSummaryHook(summaryRecorderFunc(func(event SummaryEvent) { got = append(got, event) }),
		WithNow(func() time.Time { return at }))(base)
	ctx, invocation, err := hook.Before(context.Background(), frameworktelemetry.Action{
		Type: frameworktelemetry.EventAgentStart,
		Name: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(5 * time.Minute)
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{Error: errors.New("boom")}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("summaries = %#v, want none for non llm/tool", got)
	}
}

// TestSummaryHookNilRecorder 验证 recorder 为 nil 时纯透传不 panic。
func TestSummaryHookNilRecorder(t *testing.T) {
	at := time.Unix(1000, 0)
	hook, _ := newSummaryTestHook(t, &at)
	ctx, invocation, err := hook.Before(context.Background(), frameworktelemetry.Action{
		Type: frameworktelemetry.EventToolBefore,
		Name: "bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
}
