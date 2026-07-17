package provider

import (
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

func TestWalkTree(t *testing.T) {
	count := 0
	walkTree(nil, func(_ *seelebridge.TraceNode) { count++ })
	if count != 0 {
		t.Fatal("expected 0")
	}

	root := &seelebridge.TraceNode{ID: "1", Children: []*seelebridge.TraceNode{
		{ID: "2", Children: []*seelebridge.TraceNode{{ID: "3"}}},
		{ID: "4"},
	}}
	var ids []string
	walkTree(root, func(n *seelebridge.TraceNode) { ids = append(ids, n.ID) })
	if len(ids) != 4 {
		t.Fatalf("expected 4, got %v", ids)
	}
}

func TestExtractLLMInfo_Text(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(&seelebridge.TraceNode{
		Kind: seelebridge.SpanLLMCall, Name: "call1",
		Attrs: map[string]string{"response_type": "text", "model": "gpt-4", "total_tokens": "150"},
	}, snap, "t1")
	if snap.TokenEstimate != 150 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
	if len(snap.Findings) == 0 {
		t.Fatal("expected findings")
	}
}

func TestExtractLLMInfo_ToolCalls(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(&seelebridge.TraceNode{
		Kind: seelebridge.SpanLLMCall, Name: "call2",
		Attrs: map[string]string{"response_type": "tool_calls", "tool_count": "3", "total_tokens": "500"},
	}, snap, "t2")
	if snap.TokenEstimate != 500 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
}

func TestExtractLLMInfo_NoAttrs(t *testing.T) {
	snap := &snapshot.ContextSnapshot{TokenEstimate: 100}
	extractLLMInfo(&seelebridge.TraceNode{Kind: seelebridge.SpanLLMCall, Name: "call3"}, snap, "t3")
	if snap.TokenEstimate != 100 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
}

func TestExtractToolDecision_Normal(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&seelebridge.TraceNode{
		Kind: seelebridge.SpanToolDispatch, Name: "read_file",
		Attrs: map[string]string{"tool": "read_file", "arguments": `{"path":"/x"}`},
	}, snap)
	if len(snap.Decisions) != 1 {
		t.Fatal("expected 1 decision")
	}
}

func TestExtractToolDecision_NoTool(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&seelebridge.TraceNode{Kind: seelebridge.SpanToolDispatch}, snap)
	if len(snap.Decisions) != 0 {
		t.Fatal("expected 0")
	}
}

func TestExtractToolDecision_Error(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&seelebridge.TraceNode{
		Kind: seelebridge.SpanToolDispatch, Name: "rf", Status: seelebridge.TraceStatusError,
		Attrs: map[string]string{"tool": "read_file", "error": "not found"},
	}, snap)
	if len(snap.Decisions) != 1 || snap.Decisions[0].Why == "" {
		t.Fatal("expected error in Why")
	}
}

func TestTraceProvider_NilPanic(t *testing.T) {
	defer func() { recover() }()
	NewTraceProvider(nil)
	t.Fatal("expected panic")
}
