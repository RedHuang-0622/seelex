package sessionstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestEventKindRoundTrip(t *testing.T) {
	configs := []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
	}
	for _, config := range configs {
		config := config
		t.Run(string(config.Backend), func(t *testing.T) {
			repository, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()

			key := Key{ProjectID: "project", SessionID: "session"}
			now := time.Now().UTC()
			commit := Commit{Events: []Event{
				{Seq: 1, Role: "user", Kind: EventKindUserInput, Content: "hello", CreatedAt: now},
				{Seq: 2, Role: "assistant", Kind: EventKindLLM, Content: "hi", CreatedAt: now},
				{Seq: 3, Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}, CreatedAt: now},
				{Seq: 4, Role: "tool", Kind: EventKindToolOutput, ToolCallID: "c1", Name: "bash", Content: "ok", CreatedAt: now},
			}}
			if err := repository.WriteCommit(context.Background(), key, commit); err != nil {
				t.Fatal(err)
			}

			events, err := repository.ReadEventRange(context.Background(), key, 1, 4)
			if err != nil {
				t.Fatal(err)
			}
			wantKinds := []string{EventKindUserInput, EventKindLLM, EventKindToolCall, EventKindToolOutput}
			if len(events) != len(wantKinds) {
				t.Fatalf("read %d events, want %d", len(events), len(wantKinds))
			}
			for index, event := range events {
				if event.Kind != wantKinds[index] {
					t.Fatalf("event %d kind = %q, want %q", index+1, event.Kind, wantKinds[index])
				}
			}

			tail, err := repository.ReadEventTail(context.Background(), key, 1<<20, 16)
			if err != nil || len(tail) != len(wantKinds) {
				t.Fatalf("tail len=%d err=%v, want %d", len(tail), err, len(wantKinds))
			}
		})
	}
}

func TestEventKindOfFallback(t *testing.T) {
	cases := []struct {
		event Event
		want  string
	}{
		{Event{Role: "user"}, EventKindUserInput},
		{Event{Role: "assistant"}, EventKindLLM},
		{Event{Role: "assistant", ToolCalls: []EventToolCall{{ID: "c1"}}}, EventKindToolCall},
		{Event{Role: "tool"}, EventKindToolOutput},
		{Event{Role: "system"}, EventKindSystem},
		{Event{Role: "error"}, EventKindError},
		{Event{Role: "unknown"}, EventKindNotice},
		{Event{Role: "assistant", Kind: EventKindToolCall}, EventKindToolCall},
	}
	for _, entry := range cases {
		if got := EventKindOf(entry.event); got != entry.want {
			t.Fatalf("EventKindOf(%+v) = %q, want %q", entry.event, got, entry.want)
		}
	}
}
