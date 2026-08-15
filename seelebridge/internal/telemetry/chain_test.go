package telemetry

import (
	"context"
	"errors"
	"testing"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
)

// orderSpy 记录钩子 Before/After 的调用顺序，验证 Chain 的嵌套方向。
type orderSpy struct {
	name  string
	next  frameworktelemetry.Hook
	order *[]string
}

func (hook orderSpy) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	*hook.order = append(*hook.order, hook.name+".before")
	return hook.next.Before(ctx, action)
}

func (hook orderSpy) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	*hook.order = append(*hook.order, hook.name+".after")
	return hook.next.After(ctx, invocation, effect)
}

func spyWrapper(name string, order *[]string) Wrapper {
	return func(next frameworktelemetry.Hook) frameworktelemetry.Hook {
		return orderSpy{name: name, next: next, order: order}
	}
}

// TestChainNestsOutermostFirst 验证 Chain 按从左到右第一个最外层组装：
// Before 按 外层→内层 调用，After 按 外层→内层 调用（与装饰链语义一致）。
func TestChainNestsOutermostFirst(t *testing.T) {
	var order []string
	chain := Chain(nil, spyWrapper("outer", &order), spyWrapper("inner", &order))
	ctx, invocation, err := chain.Before(context.Background(), frameworktelemetry.Action{})
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"outer.before", "inner.before", "outer.after", "inner.after"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestChainSkipsNilWrappers 验证 nil wrapper 与 nil 产出均被跳过，链不中断。
func TestChainSkipsNilWrappers(t *testing.T) {
	var order []string
	chain := Chain(nil, nil, spyWrapper("only", &order), func(frameworktelemetry.Hook) frameworktelemetry.Hook { return nil })
	ctx, invocation, err := chain.Before(context.Background(), frameworktelemetry.Action{})
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.After(ctx, invocation, frameworktelemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"only.before", "only.after"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// errorSpy 是实现 ErrorHook 的观察面：记录收到的 OnError 调用。
type errorSpy struct {
	next   frameworktelemetry.Hook
	called []string
}

func (hook *errorSpy) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	return hook.next.Before(ctx, action)
}

func (hook *errorSpy) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	return hook.next.After(ctx, invocation, effect)
}

func (hook *errorSpy) OnError(_ context.Context, name string, _ error, _ frameworktelemetry.Attributes) error {
	hook.called = append(hook.called, name)
	return nil
}

// TestChainPropagatesOnErrorInOrder 验证 OnError 集中传播：所有实现
// ErrorHook 的组成钩子按最外层→最内层顺序被调用（含最内层 LifecycleHook）。
func TestChainPropagatesOnErrorInOrder(t *testing.T) {
	tracer := NewTracer()
	base, err := NewLifecycleHook(tracer)
	if err != nil {
		t.Fatal(err)
	}
	outer := &errorSpy{next: nil}
	chain := Chain(base, func(next frameworktelemetry.Hook) frameworktelemetry.Hook {
		outer.next = next
		return outer
	})
	traceCtx, _, err := tracer.StartTrace(context.Background(), "chain-test", frameworktelemetry.SpanInternal, nil)
	if err != nil {
		t.Fatal(err)
	}
	hookErr := errors.New("boom")
	errorChain, ok := chain.(frameworktelemetry.ErrorHook)
	if !ok {
		t.Fatal("Chain does not expose ErrorHook")
	}
	if err := errorChain.OnError(traceCtx, "test.error", hookErr, nil); err != nil {
		t.Fatal(err)
	}
	if len(outer.called) != 1 || outer.called[0] != "test.error" {
		t.Fatalf("outer error hook called = %v, want [test.error]", outer.called)
	}
	view, err := tracer.Query(traceCtx, frameworktelemetry.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range view.Events {
		if event.Type == frameworktelemetry.EventError {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("LifecycleHook.OnError was not reached: no error event recorded in tracer")
	}
}
