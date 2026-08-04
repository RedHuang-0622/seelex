package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/telemetry"
)

type bashTelemetryContextKey struct{}

// diagnosticTelemetryHook adds no telemetry data of its own. It only exposes
// bounded stage markers around telemetry's existing Before/After calls so the
// backend diagnostic console can locate a stall between registry dispatch and
// the application completion hook.
type diagnosticTelemetryHook struct {
	next    telemetry.Hook
	runtime *Runtime
}

func newDiagnosticTelemetryHook(next telemetry.Hook, runtime *Runtime) telemetry.Hook {
	if next == nil {
		return nil
	}
	return &diagnosticTelemetryHook{next: next, runtime: runtime}
}

func (hook *diagnosticTelemetryHook) Before(ctx context.Context, action telemetry.Action) (context.Context, telemetry.Invocation, error) {
	isBash := action.Name == "bash"
	if isBash {
		hook.runtime.observeBash(BashDiagnosticEvent{Stage: "bash.telemetry.before.start"})
	}
	nextCtx, invocation, err := hook.next.Before(ctx, action)
	if isBash {
		if err != nil {
			hook.runtime.observeBash(BashDiagnosticEvent{Stage: "bash.telemetry.before.error", Err: err})
		} else {
			hook.runtime.observeBash(BashDiagnosticEvent{Stage: "bash.telemetry.before.done"})
		}
		nextCtx = context.WithValue(nextCtx, bashTelemetryContextKey{}, true)
	}
	return nextCtx, invocation, err
}

func (hook *diagnosticTelemetryHook) After(ctx context.Context, invocation telemetry.Invocation, effect telemetry.Effect) error {
	isBash, _ := ctx.Value(bashTelemetryContextKey{}).(bool)
	if isBash {
		hook.runtime.observeBash(BashDiagnosticEvent{Stage: "bash.telemetry.after.start"})
	}
	err := hook.next.After(ctx, invocation, effect)
	if isBash {
		if err != nil {
			hook.runtime.observeBash(BashDiagnosticEvent{Stage: "bash.telemetry.after.error", Err: err})
		} else {
			hook.runtime.observeBash(BashDiagnosticEvent{Stage: "bash.telemetry.after.done"})
		}
	}
	return err
}
