package provider

import (
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── EngineProvider ────────────────────────────────────────────

func TestEngineProvider_Name(t *testing.T) {
	p := &EngineProvider{}
	if n := p.Name(); n != "engine" {
		t.Fatalf("got %q", n)
	}
}

func TestEngineProvider_NilPanic(t *testing.T) {
	defer func() { recover() }()
	NewEngineProvider(nil)
	t.Fatal("expected panic")
}

func TestEngineProvider_NilPanicWithGoal(t *testing.T) {
	defer func() { recover() }()
	NewEngineProviderWithGoal(nil, "test")
	t.Fatal("expected panic")
}

// ── TraceProvider ─────────────────────────────────────────────

func TestTraceProvider_NilPanic(t *testing.T) {
	defer func() { recover() }()
	NewTraceProvider(nil)
	t.Fatal("expected panic")
}

func TestWalkTree(t *testing.T) {
	count := 0
	walkTree(nil, func(_ *tracer.Node) { count++ })
	if count != 0 {
		t.Fatal("expected 0")
	}

	root := &tracer.Node{
		ID: "1",
		Children: []*tracer.Node{
			{ID: "2", Children: []*tracer.Node{{ID: "3"}}},
			{ID: "4"},
		},
	}
	var ids []string
	walkTree(root, func(n *tracer.Node) { ids = append(ids, n.ID) })
	if len(ids) != 4 {
		t.Fatalf("expected 4, got %v", ids)
	}
}

func TestExtractLLMInfo_Text(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(&tracer.Node{
		Name: "call1",
		Kind: tracer.SpanLLMCall,
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
	extractLLMInfo(&tracer.Node{
		Name: "call2",
		Kind: tracer.SpanLLMCall,
		Attrs: map[string]string{"response_type": "tool_calls", "tool_count": "3", "total_tokens": "500"},
	}, snap, "t2")
	if snap.TokenEstimate != 500 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
}

func TestExtractLLMInfo_NoAttrs(t *testing.T) {
	snap := &snapshot.ContextSnapshot{TokenEstimate: 100}
	extractLLMInfo(&tracer.Node{Name: "call3", Kind: tracer.SpanLLMCall}, snap, "t3")
	if snap.TokenEstimate != 100 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
}

func TestExtractLLMInfo_InvalidToken(t *testing.T) {
	snap := &snapshot.ContextSnapshot{TokenEstimate: 0}
	extractLLMInfo(&tracer.Node{
		Name: "call4",
		Kind: tracer.SpanLLMCall,
		Attrs: map[string]string{"total_tokens": "not-a-number"},
	}, snap, "t4")
	// Invalid number should not change TokenEstimate
	if snap.TokenEstimate != 0 {
		t.Fatalf("expected 0, got %d", snap.TokenEstimate)
	}
}

func TestExtractLLMInfo_UnknownResponseType(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(&tracer.Node{
		Name: "call5",
		Kind: tracer.SpanLLMCall,
		Attrs: map[string]string{"response_type": "unknown", "model": "gpt-4"},
	}, snap, "t5")
	// Unknown response type should not add findings
	if len(snap.Findings) != 1 {
		t.Fatalf("expected 1 finding (model), got %d: %+v", len(snap.Findings), snap.Findings)
	}
}

func TestExtractToolDecision_Normal(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{
		Name: "read_file",
		Kind: tracer.SpanToolDispatch,
		Attrs: map[string]string{"tool": "read_file", "arguments": `{"path":"/x"}`},
	}, snap)
	if len(snap.Decisions) != 1 {
		t.Fatal("expected 1 decision")
	}
	if snap.Decisions[0].What != "调用工具 read_file" {
		t.Errorf("expected '调用工具 read_file', got %q", snap.Decisions[0].What)
	}
	if len(snap.Decisions[0].Alternatives) == 0 || snap.Decisions[0].Alternatives[0] == "" {
		t.Error("expected arguments in alternatives")
	}
}

func TestExtractToolDecision_NoTool(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{Kind: tracer.SpanToolDispatch}, snap)
	if len(snap.Decisions) != 0 {
		t.Fatal("expected 0")
	}
}

func TestExtractToolDecision_Error(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{
		Name:   "rf",
		Kind:   tracer.SpanToolDispatch,
		Status: tracer.StatusError,
		Attrs:  map[string]string{"tool": "read_file", "error": "not found"},
	}, snap)
	if len(snap.Decisions) != 1 {
		t.Fatal("expected 1 decision")
	}
	if snap.Decisions[0].Why == "" {
		t.Fatal("expected error in Why")
	}
	if !contains(snap.Decisions[0].Why, "not found") {
		t.Errorf("expected error message in Why, got %q", snap.Decisions[0].Why)
	}
}

func TestExtractToolDecision_NoArgs(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(&tracer.Node{
		Name: "search",
		Kind: tracer.SpanToolDispatch,
		Attrs: map[string]string{"tool": "search"},
	}, snap)
	if len(snap.Decisions) != 1 {
		t.Fatal("expected 1 decision")
	}
	if len(snap.Decisions[0].Alternatives) != 0 {
		t.Errorf("expected no alternatives when no args, got %v", snap.Decisions[0].Alternatives)
	}
}

// ── Helpers ──────────────────────────────────────────────────

func contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
