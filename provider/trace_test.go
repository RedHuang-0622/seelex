package provider

import (
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/seelex/snapshot"
)

func TestWalkTree(t *testing.T) {
	count := 0
	walkTree(nil, func(_ *tracer.Node) { count++ })
	if count != 0 { t.Fatal("expected 0") }

	root := &tracer.Node{ID: "1", Children: []*tracer.Node{
		{ID: "2", Children: []*tracer.Node{{ID: "3"}}},
		{ID: "4"},
	}}
	var ids []string
	walkTree(root, func(n *tracer.Node) { ids = append(ids, n.ID) })
	if len(ids) != 4 { t.Fatalf("expected 4, got %v", ids) }
}

func TestExtractLLMInfo_Text(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(&tracer.Node{
		Kind: tracer.SpanLLMCall, Name: "call1",
		Attrs: map[string]string{"response_type": "text", "model": "gpt-4", "total_tokens": "150"},
	}, snap, "t1")
	if snap.TokenEstimate != 150 { t.Fatalf("got %d", snap.TokenEstimate) }
	if len(snap.Findings) == 0 { t.Fatal("expected findings") }
}

func TestExtractLLMInfo_ToolCalls(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(&tracer.Node{
		Kind: tracer.SpanLLMCall, Name: "call2",
		Attrs: map[string]string{"response_type": "tool_calls", "tool_count": "3", "total_tokens": "500"},
	}, snap, "t2")
	if snap.TokenEstimate != 500 { t.Fatalf("got %d", snap.TokenEstimate) }
}

func TestExtractLLMInfo_NoAttrs(t *testing.T) {
	snap := &snapshot.ContextSnapshot{TokenEstimate: 100}
	extractLLMInfo(&tracer.Node{Kind: tracer.SpanLLMCall, Name: "call3"}, snap, "t3")
	if snap.TokenEstimate != 100 { t.Fatalf("got %d", snap.TokenEstimate) }
}

func TestExtractToolDecision_Normal(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{
		Kind: tracer.SpanToolDispatch, Name: "read_file",
		Attrs: map[string]string{"tool": "read_file", "arguments": `{"path":"/x"}`},
	}, snap)
	if len(snap.Decisions) != 1 { t.Fatal("expected 1 decision") }
}

func TestExtractToolDecision_NoTool(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{Kind: tracer.SpanToolDispatch}, snap)
	if len(snap.Decisions) != 0 { t.Fatal("expected 0") }
}

func TestExtractToolDecision_Error(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{
		Kind: tracer.SpanToolDispatch, Name: "rf", Status: tracer.StatusError,
		Attrs: map[string]string{"tool": "read_file", "error": "not found"},
	}, snap)
	if len(snap.Decisions) != 1 || snap.Decisions[0].Why == "" { t.Fatal("expected error in Why") }
}

func TestTraceProvider_NilPanic(t *testing.T) {
	defer func() { recover() }()
	NewTraceProvider(nil)
	t.Fatal("expected panic")
}
