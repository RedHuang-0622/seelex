package seelebridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	frameworkmcp "github.com/RedHuang-0622/Seele/agent/core/tool/mcp"

	"github.com/RedHuang-0622/seelex/mcpstack"
)

// ── TestBreakerEventsChannel ────────────────────────────────────

func TestBreakerEventsChannel(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	ch := r.BreakerEvents()
	if ch == nil {
		t.Fatal("BreakerEvents() returned nil channel")
	}

	// Same channel on second call
	ch2 := r.BreakerEvents()
	if ch != ch2 {
		t.Error("different channel on second call")
	}

	t.Logf("breaker events channel type: <-chan BreakerEvent")
}

// ── TestMCPStackInitialized ─────────────────────────────────────

func TestMCPStackInitialized(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	if r.MCPStack == nil {
		t.Fatal("MCPStack not initialized by NewRuntime")
	}
	if r.MCPStack.ActiveCount() != 0 {
		t.Errorf("expected empty, got %d", r.MCPStack.ActiveCount())
	}

	call := mcpstack.MCPCall{
		ServerName: "test", ToolName: "tool",
		Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess,
	}
	if err := r.MCPStack.Record(call); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if r.MCPStack.ActiveCount() != 1 {
		t.Errorf("expected 1, got %d", r.MCPStack.ActiveCount())
	}
}

// ── TestListenBreakerIntegration ────────────────────────────────

func TestListenBreakerIntegration(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	// Use a test-local channel so we can send
	ch := make(chan frameworkmcp.BreakerEvent, 64)
	go mcpstack.ListenBreaker(r.MCPStack, ch)

	events := []frameworkmcp.BreakerEvent{
		{ServerName: "freecad", Type: frameworkmcp.BreakerOpened, Failures: 3},
		{ServerName: "freecad", Type: frameworkmcp.BreakerRecovering, Failures: 3},
		{ServerName: "chem", Type: frameworkmcp.BreakerClosed, Failures: 0},
		{ServerName: "freecad", Type: frameworkmcp.BreakerRecovered, Failures: 0},
	}
	for _, evt := range events {
		ch <- evt
	}
	close(ch) // signal listener to stop (though it runs forever, this is fine for test)
	time.Sleep(50 * time.Millisecond)

	freecadEvts := r.MCPStack.ByServer("freecad")
	if len(freecadEvts) != 3 {
		t.Errorf("expected 3 freecad events, got %d", len(freecadEvts))
	}
	chemEvts := r.MCPStack.ByServer("chem")
	if len(chemEvts) != 1 {
		t.Errorf("expected 1 chem event, got %d", len(chemEvts))
	}
	if len(freecadEvts) > 0 {
		first := freecadEvts[0]
		if first.ToolName != "__breaker__opened" {
			t.Errorf("expected __breaker__opened, got %s", first.ToolName)
		}
		if first.Status != mcpstack.StatusRolledBack {
			t.Errorf("expected StatusRolledBack, got %v", first.Status)
		}
	}
}

// ── TestMCPStackSnapshotProvider ────────────────────────────────

func TestMCPStackSnapshotProvider(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	calls := []mcpstack.MCPCall{
		{ServerName: "cad", ToolName: "sketch_rect", Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess},
		{ServerName: "cad", ToolName: "pad_extrude", Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess},
		{ServerName: "web", ToolName: "search", Args: json.RawMessage(`{}`), Status: mcpstack.StatusFailed, ErrorMsg: "timeout"},
	}
	for _, call := range calls {
		r.MCPStack.Record(call)
	}

	provider := mcpstack.NewTraceProvider(r.MCPStack)
	snap, err := provider.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("nil snapshot")
	}

	summaryFound := false
	for _, f := range snap.Findings {
		if len(f) > 4 && f[:4] == "MCP " {
			summaryFound = true
		}
	}
	if !summaryFound {
		t.Errorf("findings missing MCP summary, got: %v", snap.Findings)
	}
}

// ── TestAttachMCPStackCreated ──────────────────────────────────

func TestAttachMCPStackCreated(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	// Attempt attach (will fail, but stack is still available for tracing)
	_ = r.AttachMCPServer(context.Background(), "should-fail", "stdio",
		"nonexistent-cmd", nil, nil, "")

	// Stack should still be usable for manual recording
	r.MCPStack.Record(mcpstack.MCPCall{
		ServerName: "manual", ToolName: "op",
		Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess,
	})
	if r.MCPStack.ActiveCount() != 1 {
		t.Errorf("expected 1 manual call, got %d", r.MCPStack.ActiveCount())
	}
	t.Logf("MCPStack available after AttachMCP: %d total calls", r.MCPStack.TotalCount())
}

// ── TestMCPStackConcurrent ──────────────────────────────────────

func TestMCPStackConcurrent(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	done := make(chan struct{}, 2)
	record := func() {
		for i := 0; i < 50; i++ {
			call := mcpstack.MCPCall{
				ServerName: "conc", ToolName: "tool",
				Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess,
			}
			r.MCPStack.Record(call)
			r.MCPStack.Undo()
			r.MCPStack.Redo()
		}
		done <- struct{}{}
	}
	read := func() {
		for i := 0; i < 50; i++ {
			_ = r.MCPStack.ForPrompt(200)
		}
		done <- struct{}{}
	}

	go record()
	go read()
	<-done
	<-done
	t.Logf("concurrent OK: %d total calls", r.MCPStack.TotalCount())
}

// ── TestMCPStackPersistence ─────────────────────────────────────

func TestMCPStackPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.json")

	r := newTestRuntime(t)
	defer r.Shutdown()

	r.MCPStack.Record(mcpstack.MCPCall{
		ServerName: "test", ToolName: "op1",
		Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess,
	})
	r.MCPStack.Record(mcpstack.MCPCall{
		ServerName: "test", ToolName: "op2",
		Args: json.RawMessage(`{}`), Status: mcpstack.StatusFailed, ErrorMsg: "fail",
	})

	if err := r.MCPStack.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := mcpstack.LoadStack(path)
	if err != nil {
		t.Fatalf("LoadStack: %v", err)
	}
	if loaded.ActiveCount() != 2 {
		t.Errorf("expected 2, got %d", loaded.ActiveCount())
	}
	if loaded.ByServer("test")[1].Status != mcpstack.StatusFailed {
		t.Error("expected failed status after reload")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("not valid JSON: %v", err)
	}
}

// ── TestMCPStackMultiServer ─────────────────────────────────────

func TestMCPStackMultiServer(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	for i := 0; i < 3; i++ {
		r.MCPStack.Record(mcpstack.MCPCall{
			ServerName: "cad", ToolName: "rect",
			Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess,
		})
	}
	r.MCPStack.Record(mcpstack.MCPCall{
		ServerName: "chem", ToolName: "sim",
		Args: json.RawMessage(`{}`), Status: mcpstack.StatusSuccess,
	})

	if len(r.MCPStack.ByServer("cad")) != 3 {
		t.Errorf("expected 3 cad calls, got %d", len(r.MCPStack.ByServer("cad")))
	}
	if len(r.MCPStack.ByServer("chem")) != 1 {
		t.Errorf("expected 1 chem call, got %d", len(r.MCPStack.ByServer("chem")))
	}
	if len(r.MCPStack.ByTool("rect")) != 3 {
		t.Errorf("expected 3 rect calls, got %d", len(r.MCPStack.ByTool("rect")))
	}

	latest := r.MCPStack.Latest(2)
	if len(latest) != 2 {
		t.Errorf("expected 2 latest, got %d", len(latest))
	}
}

// ── TestBreakerEventsReadonly ───────────────────────────────────

func TestBreakerEventsReadonly(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	ch := r.BreakerEvents()
	if ch == nil {
		t.Fatal("nil channel")
	}

	// Verify it's read-only by checking we can range (not send)
	done := make(chan struct{})
	go func() {
		count := 0
		for range ch {
			count++
		}
		t.Logf("received %d events from readonly channel", count)
		done <- struct{}{}
	}()

	// Can't send on ch (it's <-chan), so send via the provider's internal channel
	// by simulating a breaker event through the MCP provider.
	// Since we can't access the internal channel, just verify the type.
	time.Sleep(10 * time.Millisecond)
	t.Logf("BreakerEvents() returns <-chan BreakerEvent — consumers can only receive")
}
