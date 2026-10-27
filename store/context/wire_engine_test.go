package context

import (
	"testing"
)

type staticWireAttempts map[uint64][]WireAttempt

func (attempts staticWireAttempts) RecentForAnchor(anchorSeq uint64, k int) []WireAttempt {
	items := attempts[anchorSeq]
	if k > 0 && len(items) > k {
		items = items[len(items)-k:]
	}
	return items
}

func TestAssembleWireRowsKeepsFrameTailAndAnchoredAttempts(t *testing.T) {
	frame := &WireFrame{Summary: "compacted summary", MessageToSeq: 4}
	rows := []WireRow{
		{Seq: 5, Kind: "user_input", Content: "hello"},
		{
			Seq: 6, Kind: "tool_call", Content: "calling",
			ToolCalls: []WireToolCall{{ID: "call-1", Name: "search", Arguments: "{}"}},
		},
		{Seq: 7, Kind: "tool_output", ToolCallID: "call-1", Content: "display", ProviderContent: "wire"},
		{Seq: 8, Kind: "tool_output", ToolCallID: "orphan", Content: "drop"},
		{Seq: 9, Kind: "internal", Role: "context", Content: "material", WireMaterial: true},
	}
	attempts := staticWireAttempts{
		6: {
			{Role: WireRoleAssistant, Content: "older"},
			{Role: WireRoleAssistant, Content: "latest"},
		},
	}

	result := AssembleWireRows(frame, rows, attempts, WireParams{Budget: 10_000, K: 1})

	if result.TailStartSeq != 5 || !result.FrameApplied || result.NeedCompact || result.Open {
		t.Fatalf("result metadata=%+v", result)
	}
	want := []struct {
		role     string
		content  string
		attempt  bool
		internal bool
	}{
		{role: WireRoleAssistant, content: "compacted summary"},
		{role: WireRoleUser, content: "hello"},
		{role: WireRoleAssistant, content: "calling"},
		{role: WireRoleAssistant, content: "latest", attempt: true},
		{role: WireRoleTool, content: "wire"},
		{role: WireRoleSystem, content: "material", internal: true},
	}
	if len(result.Messages) != len(want) {
		t.Fatalf("messages=%+v", result.Messages)
	}
	for index, expected := range want {
		message := result.Messages[index]
		if message.Role != expected.role || message.Content != expected.content ||
			message.Attempt != expected.attempt || message.Internal != expected.internal {
			t.Fatalf("message[%d]=%+v want=%+v", index, message, expected)
		}
	}
}

func TestAssembleWireRowsRepairsOpenTailAndStopsAtUnitBoundary(t *testing.T) {
	rows := []WireRow{
		{
			Seq: 1, Kind: "tool_call", Content: "open", TokenCount: 1,
			ToolCalls: []WireToolCall{{ID: "call-1", Name: "search", Arguments: "{}"}},
		},
		{Seq: 2, Kind: "user_input", Content: "must not append after compact boundary"},
	}

	result := AssembleWireRows(nil, rows, nil, WireParams{Budget: 1, K: 3})

	if !result.NeedCompact || result.Open {
		t.Fatalf("result metadata=%+v", result)
	}
	if len(result.Messages) != 2 || !result.Messages[1].Repair || result.Messages[1].ToolCallID != "call-1" {
		t.Fatalf("messages=%+v", result.Messages)
	}
}

func TestAssembleWireRowsMarksOpenTail(t *testing.T) {
	rows := []WireRow{{
		Seq: 1, Kind: "tool_call", Content: "open", TokenCount: 1,
		ToolCalls: []WireToolCall{{ID: "call-1", Name: "search", Arguments: "{}"}},
	}}

	result := AssembleWireRows(nil, rows, nil, WireParams{Budget: 10_000, K: 3})

	if result.NeedCompact || !result.Open {
		t.Fatalf("result metadata=%+v", result)
	}
	if len(result.Messages) != 2 || !result.Messages[1].Repair ||
		result.Messages[1].ToolCallID != "call-1" {
		t.Fatalf("messages=%+v", result.Messages)
	}
}

func TestWireDigestAndPrefixIgnoreAttempts(t *testing.T) {
	persistent := []WireMessage{
		{Role: WireRoleUser, Content: "hello", Seq: 1},
		{Role: WireRoleAssistant, Content: "answer", Seq: 2},
	}
	withAttempt := append(
		append([]WireMessage(nil), persistent[:1]...),
		WireMessage{Role: WireRoleAssistant, Content: "retry", Seq: 1, Attempt: true},
	)
	withAttempt = append(withAttempt, persistent[1])

	if WireDigest(persistent) == "" || WireDigest(persistent) != WireDigest(append([]WireMessage(nil), persistent...)) {
		t.Fatal("digest is not stable")
	}
	if !WirePrefixEqual(persistent, withAttempt) {
		t.Fatal("attempt rows must not affect persistent prefix comparison")
	}
}

var _ WireAttemptProvider = staticWireAttempts{}
