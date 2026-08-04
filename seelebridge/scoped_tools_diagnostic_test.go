package seelebridge

import (
	"context"
	"strings"
	"testing"
)

func TestScopedBashPublishesDiagnosticStages(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	var events []BashDiagnosticEvent
	runtime.SetBashDiagnosticObserver(func(event BashDiagnosticEvent) {
		events = append(events, event)
	})
	output, err := runtime.scopedBash(context.Background(), `{"command":"echo diagnostic-ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "diagnostic-ok") {
		t.Fatalf("bash output = %q", output)
	}

	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Stage)
		if event.Shell == "diagnostic-ok" {
			t.Fatal("diagnostic observer exposed command content as shell metadata")
		}
	}
	want := []string{
		"bash.resolve.start",
		"bash.resolve.done",
		"bash.command.prepared",
		"bash.process.starting",
		"bash.process.started",
		"bash.process.exited",
		"bash.handler.return",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("diagnostic stages = %v, want %v", got, want)
	}
}

func TestBashDiagnosticObserverPanicDoesNotBreakTool(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	runtime.SetBashDiagnosticObserver(func(BashDiagnosticEvent) { panic("test observer") })
	if _, err := runtime.scopedBash(context.Background(), `{"command":"echo observer-safe"}`); err != nil {
		t.Fatalf("panic in diagnostic observer must not break bash: %v", err)
	}
}
