package conversation_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
)

func TestConversationTurnCleanupRunsFullSuccessfulPath(t *testing.T) {
	var calls []string
	projection := view_state.RuntimeStateProjection{}
	cleanup := applicationconversation.NewTurnCleanup(applicationconversation.TurnCleanupDeps{
		StampRoute:     func(string, string) { calls = append(calls, "route") },
		DiscardMailbox: func(string) { calls = append(calls, "mailbox") },
		CollectProjection: func(string) view_state.RuntimeStateProjection {
			calls = append(calls, "projection")
			return projection
		},
		RemoveCheckpoints: func(string) error { calls = append(calls, "checkpoints"); return nil },
		RemoveRecovery:    func(string) error { calls = append(calls, "recovery"); return nil },
		RecordError:       func(string, string, error) { t.Fatal("成功路径不得记录未处理错误") },
	})

	got, err := cleanup.Cleanup("session-a", "turn-a", nil,
		func() error { calls = append(calls, "flush"); return nil },
		func() { calls = append(calls, "stop-draft") },
	)

	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if !reflect.DeepEqual(got, projection) {
		t.Fatalf("projection = %+v", got)
	}
	want := "flush,route,mailbox,projection,checkpoints,recovery,stop-draft"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestConversationTurnCleanupCombinesErrorsAndKeepsRunning(t *testing.T) {
	turnErr := errors.New("turn error")
	checkpointErr := errors.New("checkpoint failed")
	var recorded error
	var calls []string
	cleanup := applicationconversation.NewTurnCleanup(applicationconversation.TurnCleanupDeps{
		StampRoute:     func(string, string) { calls = append(calls, "route") },
		DiscardMailbox: func(string) { calls = append(calls, "mailbox") },
		CollectProjection: func(string) view_state.RuntimeStateProjection {
			calls = append(calls, "projection")
			return view_state.RuntimeStateProjection{}
		},
		RemoveCheckpoints: func(string) error { calls = append(calls, "checkpoints"); return checkpointErr },
		RemoveRecovery: func(string) error {
			t.Fatal("turnErr 非空时不得继续 recovery cleanup")
			return nil
		},
		RecordError: func(string, string, error) {
			calls = append(calls, "record")
			recorded = turnErr
		},
	})

	_, err := cleanup.Cleanup("session-a", "turn-a", turnErr,
		func() error { return errors.New("flush failed") },
		func() { calls = append(calls, "stop-draft") },
	)

	if !errors.Is(err, turnErr) || recorded != turnErr {
		t.Fatalf("Cleanup error = %v recorded=%v", err, recorded)
	}
	want := "route,mailbox,projection,checkpoints,record,stop-draft"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestConversationTurnCleanupWrapsFlushAndRecoveryErrors(t *testing.T) {
	flushErr := errors.New("flush")
	recoveryErr := errors.New("recovery")
	tests := []struct {
		name     string
		flush    error
		recovery error
		want     error
		text     string
	}{
		{name: "flush", flush: flushErr, want: flushErr, text: "flush streamed response"},
		{name: "recovery", recovery: recoveryErr, want: recoveryErr, text: "recovery"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanup := applicationconversation.NewTurnCleanup(applicationconversation.TurnCleanupDeps{
				StampRoute:        func(string, string) {},
				DiscardMailbox:    func(string) {},
				CollectProjection: func(string) view_state.RuntimeStateProjection { return view_state.RuntimeStateProjection{} },
				RemoveCheckpoints: func(string) error { return nil },
				RemoveRecovery:    func(string) error { return test.recovery },
				RecordError:       func(string, string, error) {},
			})
			_, err := cleanup.Cleanup("session-a", "turn-a", nil,
				func() error { return test.flush },
				func() {},
			)
			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Cleanup error = %v", err)
			}
		})
	}
}
