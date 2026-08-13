package seelebridge

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/telemetry"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

func TestDiagnosticTelemetryHookMarksBashBeforeAndAfter(t *testing.T) {
	runtime := &Runtime{}
	var stages []string
	runtime.SetBashDiagnosticObserver(func(event BashDiagnosticEvent) {
		stages = append(stages, event.Stage)
	})
	base, err := seeletelemetry.NewLifecycleHook(seeletelemetry.NewTracer())
	if err != nil {
		t.Fatal(err)
	}
	hook := newDiagnosticTelemetryHook(base, runtime)
	ctx, invocation, err := hook.Before(context.Background(), telemetry.Action{Type: telemetry.EventToolBefore, Name: "bash"})
	if err != nil {
		t.Fatal(err)
	}
	if err := hook.After(ctx, invocation, telemetry.Effect{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"bash.telemetry.before.start", "bash.telemetry.before.done", "bash.telemetry.after.start", "bash.telemetry.after.done"}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("telemetry diagnostic stages = %v, want %v", stages, want)
	}
}
