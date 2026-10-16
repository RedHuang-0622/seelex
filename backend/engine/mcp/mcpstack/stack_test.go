package mcpstack

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	s := New(WithSessionID("test-001"), WithMetadata(StackMetadata{
		SessionGoal: "Test MCP middleware",
		Domain:      "cad",
	}))
	if s.SessionID != "test-001" {
		t.Errorf("expected session test-001, got %s", s.SessionID)
	}
	if s.ActiveCount() != 0 {
		t.Errorf("expected empty stack, got %d calls", s.ActiveCount())
	}
	if s.Metadata.Domain != "cad" {
		t.Errorf("expected domain cad, got %s", s.Metadata.Domain)
	}
}

func TestRecordAndActiveCount(t *testing.T) {
	s := New()
	call1 := makeCall("freecad", "sketch_rectangle", `{"plane":"XY"}`)
	call2 := makeCall("freecad", "pad_extrude", `{"height":30}`)

	if err := s.Record(call1); err != nil {
		t.Fatalf("record call1: %v", err)
	}
	if s.ActiveCount() != 1 {
		t.Errorf("expected count 1, got %d", s.ActiveCount())
	}

	if err := s.Record(call2); err != nil {
		t.Fatalf("record call2: %v", err)
	}
	if s.ActiveCount() != 2 {
		t.Errorf("expected count 2, got %d", s.ActiveCount())
	}
}

func TestUndoRedo(t *testing.T) {
	s := New()
	recordN(s, t, 3)

	cur, err := s.Current()
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if cur.Seq != 3 {
		t.Errorf("expected seq 3, got %d", cur.Seq)
	}

	// Undo
	undone, err := s.Undo()
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if undone.Seq != 3 {
		t.Errorf("expected undone seq 3, got %d", undone.Seq)
	}
	if s.ActiveCount() != 2 {
		t.Errorf("expected count 2 after undo, got %d", s.ActiveCount())
	}

	// Redo
	redone, err := s.Redo()
	if err != nil {
		t.Fatalf("redo: %v", err)
	}
	if redone.Seq != 3 {
		t.Errorf("expected redone seq 3, got %d", redone.Seq)
	}
	if s.ActiveCount() != 3 {
		t.Errorf("expected count 3 after redo, got %d", s.ActiveCount())
	}
}

func TestUndoAtBottom(t *testing.T) {
	s := New()
	recordN(s, t, 1)

	_, err := s.Undo()
	if err != nil {
		t.Fatalf("first undo: %v", err)
	}
	if s.ActiveCount() != 0 {
		t.Errorf("expected count 0, got %d", s.ActiveCount())
	}

	_, err = s.Undo()
	if err != ErrStackBottom {
		t.Errorf("expected ErrStackBottom, got %v", err)
	}
}

func TestEmptyStackCurrent(t *testing.T) {
	s := New()
	_, err := s.Current()
	if err != ErrEmptyStack {
		t.Errorf("expected ErrEmptyStack, got %v", err)
	}
}

func TestRecordAfterUndo(t *testing.T) {
	s := New()
	recordN(s, t, 3)

	s.Undo() // [1,2], idx=1
	s.Undo() // [1], idx=0

	newCall := makeCall("chem-sim", "simulate_binding", `{"protein":"BRD4"}`)
	if err := s.Record(newCall); err != nil {
		t.Fatalf("record after undo: %v", err)
	}

	if s.TotalCount() != 2 {
		t.Errorf("expected 2 total after branching record, got %d", s.TotalCount())
	}
	if s.ActiveCount() != 2 {
		t.Errorf("expected 2 active, got %d", s.ActiveCount())
	}
	if s.Calls[1].ServerName != "chem-sim" {
		t.Errorf("expected server chem-sim, got %s", s.Calls[1].ServerName)
	}
}

func TestSerialization(t *testing.T) {
	s := New(WithSessionID("serial-test"))
	recordN(s, t, 3)

	data, err := s.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s2 := New()
	if err := s2.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if s2.SessionID != "serial-test" {
		t.Errorf("session id mismatch")
	}
	if s2.ActiveCount() != 3 {
		t.Errorf("expected 3 calls, got %d", s2.ActiveCount())
	}
	if s2.Calls[0].ToolName != "sketch_rectangle" {
		t.Errorf("expected sketch_rectangle, got %s", s2.Calls[0].ToolName)
	}
	if s2.Calls[1].Status != StatusSuccess {
		t.Errorf("expected status success, got %v", s2.Calls[1].Status)
	}
}

func TestFilePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mcpstack.json")

	s := New(WithSessionID("file-test"))
	recordN(s, t, 5)

	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	s2, err := LoadStack(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s2.ActiveCount() != 5 {
		t.Errorf("expected 5 calls, got %d", s2.ActiveCount())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("saved file not valid JSON: %v", err)
	}
}

func TestAutoSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auto-save.mcpstack.json")

	s := New(WithSessionID("auto-save"), WithAutoSave(path))
	call := makeCall("freecad", "sketch_rectangle", `{"plane":"XY"}`)
	if err := s.Record(call); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("auto-save file not created")
	}
}

func TestByServer(t *testing.T) {
	s := New()
	s.Record(makeCall("freecad", "sketch_rect", `{}`))
	s.Record(makeCall("chem-sim", "simulate", `{}`))
	s.Record(makeCall("freecad", "pad_extrude", `{}`))

	freecadCalls := s.ByServer("freecad")
	if len(freecadCalls) != 2 {
		t.Errorf("expected 2 freecad calls, got %d", len(freecadCalls))
	}
}

func TestByTool(t *testing.T) {
	s := New()
	s.Record(makeCall("freecad", "sketch_rect", `{}`))
	s.Record(makeCall("freecad", "pad_extrude", `{}`))
	s.Record(makeCall("freecad", "sketch_rect", `{}`))

	rectCalls := s.ByTool("sketch_rect")
	if len(rectCalls) != 2 {
		t.Errorf("expected 2 sketch_rect calls, got %d", len(rectCalls))
	}
}

func TestLatest(t *testing.T) {
	s := New()
	recordN(s, t, 5)

	latest := s.Latest(3)
	if len(latest) != 3 {
		t.Errorf("expected 3 latest, got %d", len(latest))
	}
	if latest[0].Seq != 3 || latest[2].Seq != 5 {
		t.Errorf("expected seqs 3-5, got %d-%d", latest[0].Seq, latest[2].Seq)
	}
}

func TestForPrompt(t *testing.T) {
	s := New(WithMetadata(StackMetadata{SessionGoal: "Test session"}))
	recordN(s, t, 3)

	prompt := s.ForPrompt(500)
	if prompt == "" {
		t.Fatal("empty prompt")
	}
	if !contains(prompt, "Test session") {
		t.Errorf("prompt should contain session goal")
	}
	if !contains(prompt, "sketch_rectangle") {
		t.Errorf("prompt should contain tool names")
	}
}

func TestSnapshot(t *testing.T) {
	s := New()
	recordN(s, t, 3)

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.ActiveCount() != 3 {
		t.Errorf("expected 3 in snapshot, got %d", snap.ActiveCount())
	}

	s.Undo()
	if s.ActiveCount() != 2 {
		t.Errorf("expected original 2 after undo, got %d", s.ActiveCount())
	}
	if snap.ActiveCount() != 3 {
		t.Errorf("expected snapshot still 3, got %d", snap.ActiveCount())
	}
}

func TestInterceptor(t *testing.T) {
	s := New()

	// Simulate an MCP call lifecycle using the interceptor
	args := json.RawMessage(`{"plane":"XY","length":100}`)
	rec := BeforeCall(s, "freecad", "sketch_rectangle", args, "")

	if s.ActiveCount() != 1 {
		t.Errorf("expected 1 pending call, got %d", s.ActiveCount())
	}

	// Simulate successful execution
	result := json.RawMessage(`{"sketch_id":1,"status":"ok"}`)
	rec.AfterCall(result, nil)

	cur, _ := s.Current()
	if cur.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", cur.Status)
	}
	if string(cur.Result) != `{"sketch_id":1,"status":"ok"}` {
		t.Errorf("unexpected result: %s", string(cur.Result))
	}
}

func TestInterceptorFailure(t *testing.T) {
	s := New()

	rec := BeforeCall(s, "freecad", "bad_tool", json.RawMessage(`{}`), "")
	rec.AfterCall(nil, errors.New("execution failed: test error"))

	cur, _ := s.Current()
	if cur.Status != StatusFailed {
		t.Errorf("expected status failed, got %v", cur.Status)
	}
	if cur.ErrorMsg == "" {
		t.Errorf("expected error message, got empty")
	}
}

func TestFormatSummary(t *testing.T) {
	s := New()
	summary := FormatSummary(s)
	if summary != "MCP 调用栈为空" {
		t.Errorf("expected empty summary, got %s", summary)
	}

	recordN(s, t, 2)
	summary = FormatSummary(s)
	if !contains(summary, "MCP trace") {
		t.Errorf("expected trace summary, got %s", summary)
	}
	if !contains(summary, "freecad") {
		t.Errorf("summary should contain server name")
	}
}

func TestMultiServer(t *testing.T) {
	s := New()
	s.Record(makeCall("cad", "sketch_rect", `{}`))
	s.Record(makeCall("cad", "pad_extrude", `{}`))
	s.Record(makeCall("chem", "simulate", `{}`))
	s.Record(makeCall("bio", "fold_protein", `{}`))

	cadCalls := s.ByServer("cad")
	chemCalls := s.ByServer("chem")
	bioCalls := s.ByServer("bio")

	if len(cadCalls) != 2 {
		t.Errorf("expected 2 cad calls, got %d", len(cadCalls))
	}
	if len(chemCalls) != 1 {
		t.Errorf("expected 1 chem call, got %d", len(chemCalls))
	}
	if len(bioCalls) != 1 {
		t.Errorf("expected 1 bio call, got %d", len(bioCalls))
	}
}

// ── Helpers ─────────────────────────────────────────────────────

func makeCall(server, tool, argsJSON string) MCPCall {
	return MCPCall{
		ID:         tool + "-" + server,
		Timestamp:  time.Now().UTC(),
		ServerName: server,
		ToolName:   tool,
		Args:       json.RawMessage(argsJSON),
		Status:     StatusSuccess,
		TokenCount: 20,
	}
}

func recordN(s *MCPStack, t *testing.T, n int) {
	t.Helper()
	servers := []string{"freecad", "freecad", "chem-sim", "freecad", "bio-sim"}
	tools := []string{"sketch_rectangle", "pad_extrude", "simulate_binding", "fillet", "fold_protein"}
	args := []string{
		`{"plane":"XY","length":100,"width":50}`,
		`{"sketch_seq":1,"height":30}`,
		`{"protein":"BRD4"}`,
		`{"edge_ids":[1],"radius":5}`,
		`{"sequence":"ATCG"}`,
	}
	for i := 0; i < n; i++ {
		call := makeCall(servers[i%len(servers)], tools[i%len(tools)], args[i%len(args)])
		if err := s.Record(call); err != nil {
			t.Fatalf("record %d: %v", i+1, err)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
