package toolhooks

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/RedHuang-0622/Seele/session"
)

func TestBridgePairsConcurrentToolCallsByFIFO(t *testing.T) {
	info := session.ToolCallInfo{Turn: 3, Name: "read_file", Arguments: `{"path":"same"}`}
	var starts []string
	var completes []string
	bridge := New()
	bridge.Bind(Deps{
		HandleToolStart: func(_ context.Context, name, id, arguments string) {
			if name != info.Name || arguments != info.Arguments {
				t.Fatalf("start info drift: name=%q arguments=%q", name, arguments)
			}
			starts = append(starts, id)
		},
		HandleToolComplete: func(_ context.Context, got session.ToolCallInfo, id string, _ func(string)) {
			if !reflect.DeepEqual(got, info) {
				t.Fatalf("complete info drift: got=%#v want=%#v", got, info)
			}
			completes = append(completes, id)
		},
	})

	hooks := bridge.Hooks()
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)

	if got, want := starts, []string{"tool-1", "tool-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start IDs = %v, want %v", got, want)
	}
	if got, want := completes, []string{"tool-1", "tool-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("complete IDs = %v, want FIFO pairing %v", got, want)
	}
}

func TestBridgeIterationGatePreservesQueuedAndHistorySemantics(t *testing.T) {
	historyErr := errors.New("repair failed")
	tests := []struct {
		name           string
		allow          bool
		sessionBacked  bool
		queued         bool
		prepareErr     error
		wantContinue   bool
		wantPrepared   bool
		wantContextErr bool
	}{
		{
			name:         "iteration budget stops before all other work",
			allow:        false,
			wantContinue: false,
		},
		{
			name:          "session backed waits while input is queued",
			allow:         true,
			sessionBacked: true,
			queued:        true,
			wantContinue:  false,
		},
		{
			name:          "session backed skips provider repair",
			allow:         true,
			sessionBacked: true,
			wantContinue:  true,
		},
		{
			name:           "history repair failure records context failure",
			allow:          true,
			prepareErr:     historyErr,
			wantContinue:   false,
			wantPrepared:   true,
			wantContextErr: true,
		},
		{
			name:         "history repair success continues",
			allow:        true,
			wantContinue: true,
			wantPrepared: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var prepared bool
			var contextErr bool
			bridge := New()
			bridge.Bind(Deps{
				AllowNextReActIteration: func(context.Context, int) bool { return tt.allow },
				SessionBacked:           func() bool { return tt.sessionBacked },
				QueuedInputs:            func() bool { return tt.queued },
				ActiveRequestID:         func() string { return "request-1" },
				PrepareHistory: func(context.Context) error {
					prepared = true
					return tt.prepareErr
				},
				RecordContextFailure: func(requestID string, err error) {
					contextErr = requestID == "request-1" && errors.Is(err, historyErr)
				},
			})

			if got := bridge.Hooks().OnIterationComplete(context.Background(), 2); got != tt.wantContinue {
				t.Fatalf("continue = %v, want %v", got, tt.wantContinue)
			}
			if prepared != tt.wantPrepared {
				t.Fatalf("prepared history = %v, want %v", prepared, tt.wantPrepared)
			}
			if contextErr != tt.wantContextErr {
				t.Fatalf("context failure recorded = %v, want %v", contextErr, tt.wantContextErr)
			}
		})
	}
}

func TestBridgeDiagnosticObserverIsBestEffort(t *testing.T) {
	var stages []string
	bridge := New()
	bridge.Bind(Deps{})
	bridge.SetDiagnosticObserver(func(event DiagnosticEvent) {
		stages = append(stages, event.Stage)
		panic("observer failure must not escape")
	})

	bridge.Hooks().OnToolStart(context.Background(), session.ToolCallInfo{Name: "read_file"})

	want := []string{"toolhook.start.enter", "toolhook.start.matched"}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("diagnostic stages = %v, want %v", stages, want)
	}
}
