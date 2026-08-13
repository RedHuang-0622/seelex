package telemetry

import (
	"context"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/tools"
)

type bashTelemetryContextKey struct{}

// DiagnosticHook adds no telemetry data of its own. It only exposes
// bounded stage markers around telemetry's existing Before/After calls so the
// backend diagnostic console can locate a stall between registry dispatch and
// the application completion hook.
type DiagnosticHook struct {
	next    frameworktelemetry.Hook
	observe func(event tools.BashDiagnosticEvent)
}

// NewDiagnosticHook 构造诊断钩子；observe 由 Runtime 注入（bash 诊断观察者）。
func NewDiagnosticHook(next frameworktelemetry.Hook, observe func(event tools.BashDiagnosticEvent)) frameworktelemetry.Hook {
	if next == nil {
		return nil
	}
	return &DiagnosticHook{next: next, observe: observe}
}

// Before 实现 telemetry.Hook。
func (hook *DiagnosticHook) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	isBash := action.Name == "bash"
	if isBash {
		hook.observe(tools.BashDiagnosticEvent{Stage: "bash.telemetry.before.start"})
	}
	nextCtx, invocation, err := hook.next.Before(ctx, action)
	if isBash {
		if err != nil {
			hook.observe(tools.BashDiagnosticEvent{Stage: "bash.telemetry.before.error", Err: err})
		} else {
			hook.observe(tools.BashDiagnosticEvent{Stage: "bash.telemetry.before.done"})
		}
		nextCtx = context.WithValue(nextCtx, bashTelemetryContextKey{}, true)
	}
	return nextCtx, invocation, err
}

// After 实现 telemetry.Hook。
func (hook *DiagnosticHook) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	isBash, _ := ctx.Value(bashTelemetryContextKey{}).(bool)
	if isBash {
		hook.observe(tools.BashDiagnosticEvent{Stage: "bash.telemetry.after.start"})
	}
	err := hook.next.After(ctx, invocation, effect)
	if isBash {
		if err != nil {
			hook.observe(tools.BashDiagnosticEvent{Stage: "bash.telemetry.after.error", Err: err})
		} else {
			hook.observe(tools.BashDiagnosticEvent{Stage: "bash.telemetry.after.done"})
		}
	}
	return err
}
