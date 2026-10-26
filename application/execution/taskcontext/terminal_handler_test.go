package task_context

import (
	"context"
	"reflect"
	"testing"
)

func TestTerminalHandlerPreservesProjectionOrder(t *testing.T) {
	var calls []string
	application := NewTerminalHandler(TerminalHandlerDeps{
		EnsurePlanProjection: func(context.Context) {
			calls = append(calls, "ensure")
		},
		VerifyAndApply: func(_ context.Context, kind, _ string) (string, error) {
			calls = append(calls, "verify:"+kind)
			return "ok", nil
		},
		MirrorTask: func(context.Context) {
			calls = append(calls, "task")
		},
		MirrorPlan: func(context.Context) {
			calls = append(calls, "plan")
		},
	})

	handler := application.Handler(ToolCheckNode)
	if result, err := handler(context.Background(), "{}"); err != nil || result != "ok" {
		t.Fatalf("handler = %q, %v", result, err)
	}
	want := []string{"ensure", "verify:" + ToolCheckNode, "task", "plan"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}

	calls = nil
	if _, err := application.Handler(ToolFailed)(context.Background(), "{}"); err != nil {
		t.Fatal(err)
	}
	want = []string{"ensure", "verify:" + ToolFailed, "task"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("failed calls = %v, want %v", calls, want)
	}
}
