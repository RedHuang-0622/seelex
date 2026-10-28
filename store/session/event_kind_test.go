package session

import (
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// TestEventKindOfExplicitWins 钉住"显式类别优先"：Kind 非空时不得被 Role 回退判定覆盖。
func TestEventKindOfExplicitWins(t *testing.T) {
	explicit := []string{
		protocol.TranscriptEventKindUserInput,
		protocol.TranscriptEventKindInternal,
		protocol.TranscriptEventKindLLM,
		protocol.TranscriptEventKindToolCall,
		protocol.TranscriptEventKindToolOutput,
		protocol.TranscriptEventKindSystem,
		protocol.TranscriptEventKindError,
		protocol.TranscriptEventKindNotice,
	}
	for _, kind := range explicit {
		// role 故意取一个会回退成 notice 的值，证明显式值确实赢了。
		event := protocol.TranscriptEvent{Role: "unknown", Kind: kind}
		if got := EventKindOf(event); got != kind {
			t.Fatalf("EventKindOf(kind=%q) = %q，显式类别必须优先", kind, got)
		}
	}
	// assistant + tool_calls 也要被显式 internal 压住（工具调用不改变显式类别）。
	if got := EventKindOf(protocol.TranscriptEvent{
		Role: "assistant", Kind: protocol.TranscriptEventKindInternal,
		ToolCalls: []protocol.TranscriptToolCall{{ID: "c1", Name: "bash"}},
	}); got != protocol.TranscriptEventKindInternal {
		t.Fatalf("显式 internal 被 ToolCalls 覆盖为 %q", got)
	}
}

// TestEventKindOfFallback 钉住旧数据（Kind 为空）的回退判据：
// role user/assistant(±tool_calls)/tool/system/error → 对应类别，其它 role → notice。
func TestEventKindOfFallback(t *testing.T) {
	cases := []struct {
		name  string
		event protocol.TranscriptEvent
		want  string
	}{
		{name: "user", event: protocol.TranscriptEvent{Role: "user"}, want: protocol.TranscriptEventKindUserInput},
		{name: "assistant", event: protocol.TranscriptEvent{Role: "assistant"}, want: protocol.TranscriptEventKindLLM},
		{
			name:  "assistant-with-tool-calls",
			event: protocol.TranscriptEvent{Role: "assistant", ToolCalls: []protocol.TranscriptToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}},
			want:  protocol.TranscriptEventKindToolCall,
		},
		{name: "tool", event: protocol.TranscriptEvent{Role: "tool"}, want: protocol.TranscriptEventKindToolOutput},
		{name: "system", event: protocol.TranscriptEvent{Role: "system"}, want: protocol.TranscriptEventKindSystem},
		{name: "error", event: protocol.TranscriptEvent{Role: "error"}, want: protocol.TranscriptEventKindError},
		{name: "unknown-role", event: protocol.TranscriptEvent{Role: "orchestrator"}, want: protocol.TranscriptEventKindNotice},
		{name: "empty-role", event: protocol.TranscriptEvent{}, want: protocol.TranscriptEventKindNotice},
	}
	for _, entry := range cases {
		entry := entry
		t.Run(entry.name, func(t *testing.T) {
			if got := EventKindOf(entry.event); got != entry.want {
				t.Fatalf("EventKindOf(%+v) = %q, want %q", entry.event, got, entry.want)
			}
		})
	}
}

// TestEventKindStringsAreStable 钉住字面值：词表住在 contract/protocol，判据不得自己造串。
func TestEventKindStringsAreStable(t *testing.T) {
	pairs := map[string]string{
		protocol.TranscriptEventKindUserInput:  "user_input",
		protocol.TranscriptEventKindInternal:   "internal",
		protocol.TranscriptEventKindLLM:        "llm",
		protocol.TranscriptEventKindToolCall:   "tool_call",
		protocol.TranscriptEventKindToolOutput: "tool_output",
		protocol.TranscriptEventKindSystem:     "system",
		protocol.TranscriptEventKindError:      "error",
		protocol.TranscriptEventKindNotice:     "notice",
	}
	for got, want := range pairs {
		if got != want {
			t.Fatalf("词表漂移：%q != %q", got, want)
		}
	}
}
