package provider

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/telemetry"
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
	NewTraceProvider(nil, "sess-1")
	t.Fatal("expected panic")
}

func TestTraceProvider_ExportReadsTelemetryLifecycleEvents(t *testing.T) {
	src := fakeTraceSource{view: telemetry.ViewModel{
		Events: []telemetry.Event{
			{
				Type: telemetry.EventLLMAfter, Status: telemetry.StatusOK,
				Name: "completion", TraceID: "t1", SpanID: "s1",
				Attributes: telemetry.Attributes{
					telemetry.AttributeGenAIRequestModel: "gpt-4",
					telemetry.AttributeGenAIUsageInput:   100,
					telemetry.AttributeGenAIUsageOutput:  50,
				},
			},
			{
				Type: telemetry.EventToolAfter, Status: telemetry.StatusOK,
				Name: "read_file", TraceID: "t1", SpanID: "s2",
				Attributes: telemetry.Attributes{
					telemetry.AttributeGenAIToolName: "read_file",
				},
			},
		},
	}}
	p := NewTraceProviderWithGoal(src, "sess-1", "测试目标")
	snap, err := p.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.SourceSessionID != "sess-1" || snap.Goal != "测试目标" {
		t.Fatalf("snap = %#v", snap)
	}
	if snap.TokenEstimate != 150 {
		t.Fatalf("token estimate = %d, want 150", snap.TokenEstimate)
	}
	if len(snap.Findings) == 0 || !contains(snap.Findings[0], "gpt-4") {
		t.Fatalf("findings = %v", snap.Findings)
	}
	if len(snap.Decisions) != 1 || snap.Decisions[0].What != "调用工具 read_file" {
		t.Fatalf("decisions = %#v", snap.Decisions)
	}
}

func TestTraceProvider_ExportEmptyEvents(t *testing.T) {
	p := NewTraceProvider(fakeTraceSource{view: telemetry.ViewModel{}}, "sess-1")
	snap, err := p.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Decisions != nil || snap.Findings != nil {
		t.Fatalf("expected nil projections, got %#v", snap)
	}
}

func TestExtractLLMInfo_Tokens(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(telemetry.Event{
		Type: telemetry.EventLLMAfter, Name: "completion",
		Attributes: telemetry.Attributes{
			telemetry.AttributeGenAIUsageInput:  100,
			telemetry.AttributeGenAIUsageOutput: 50,
		},
	}, snap)
	if snap.TokenEstimate != 150 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
}

func TestExtractLLMInfo_NoAttrs(t *testing.T) {
	snap := &snapshot.ContextSnapshot{TokenEstimate: 100}
	extractLLMInfo(telemetry.Event{Type: telemetry.EventLLMAfter, Name: "completion"}, snap)
	if snap.TokenEstimate != 100 {
		t.Fatalf("got %d", snap.TokenEstimate)
	}
}

func TestExtractLLMInfo_ModelFinding(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractLLMInfo(telemetry.Event{
		Type: telemetry.EventLLMAfter, Name: "completion", TraceID: "t1",
		Attributes: telemetry.Attributes{telemetry.AttributeGenAIRequestModel: "gpt-4"},
	}, snap)
	if len(snap.Findings) != 1 || !contains(snap.Findings[0], "gpt-4") {
		t.Fatalf("findings = %v", snap.Findings)
	}
}

func TestExtractToolDecision_Normal(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(telemetry.Event{
		Type: telemetry.EventToolAfter, Name: "read_file",
		Attributes: telemetry.Attributes{telemetry.AttributeGenAIToolName: "read_file"},
	}, snap)
	if len(snap.Decisions) != 1 {
		t.Fatal("expected 1 decision")
	}
	if snap.Decisions[0].What != "调用工具 read_file" {
		t.Errorf("expected '调用工具 read_file', got %q", snap.Decisions[0].What)
	}
}

func TestExtractToolDecision_NoTool(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(telemetry.Event{Type: telemetry.EventToolAfter, Name: ""}, snap)
	if len(snap.Decisions) != 0 {
		t.Fatal("expected 0")
	}
}

func TestExtractToolDecision_Error(t *testing.T) {
	snap := &snapshot.ContextSnapshot{}
	extractToolDecision(telemetry.Event{
		Type: telemetry.EventToolAfter, Name: "rf", Status: telemetry.StatusError,
		Attributes: telemetry.Attributes{telemetry.AttributeGenAIToolName: "read_file"},
		Error:      &telemetry.ErrorInfo{Message: "not found"},
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

// fakeTraceSource 是 TraceSource 的测试替身（telemetry.ViewModel 静态投影）。
type fakeTraceSource struct {
	view telemetry.ViewModel
}

func (f fakeTraceSource) Query(context.Context, telemetry.Query) (telemetry.ViewModel, error) {
	return f.view, nil
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
