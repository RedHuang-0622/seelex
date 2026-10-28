package toolhooks

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestProjectionStartOwnsSessionFallbackAndPublicationOrder(t *testing.T) {
	var calls []string
	projection := NewProjection(ProjectionDeps{
		Lock:                   &sync.Mutex{},
		SessionIDFromContext:   func(context.Context) string { return "" },
		CurrentSessionIDLocked: func() string { return "session-a" },
		IsActiveLocked:         func(string) bool { return true },
		Flush:                  func(string) { calls = append(calls, "flush") },
		EnsureToolTranscript: func(string, string, string, string) {
			calls = append(calls, "transcript")
		},
		AttributeNarration: func(string, string, string, string, string) {
			calls = append(calls, "narration")
		},
		StreamedAssistantText: func(string) string { return "stream" },
		AppendToolLocked: func(sessionID, name, id, arguments string, active bool) model.Message {
			calls = append(calls, "append")
			return model.Message{Role: "tool"}
		},
		BumpRevision:        func() uint64 { calls = append(calls, "bump"); return 3 },
		CurrentRequestID:    func() string { return "turn-a" },
		BackgroundRequestID: func(string) string { return "" },
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, _ any) {
			calls = append(calls, string(kind)+":"+sessionID)
		},
		RefreshWorkTable: func() { calls = append(calls, "worktable") },
		PublishRuntime: func(sessionID, requestID string) {
			calls = append(calls, "runtime:"+sessionID)
		},
	})

	projection.Start(context.Background(), "read_file", "call-1", `{}`)

	want := "flush,transcript,narration,append,bump,tool.started:session-a,worktable,runtime:session-a"
	if got := strings.Join(calls, ","); got != want {
		t.Fatalf("calls = %s\nwant  = %s", got, want)
	}
}
