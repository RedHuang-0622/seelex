package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

func TestContextStateRoundTrip(t *testing.T) {
	layout := storelayout.New(t.TempDir())
	scope := storelayout.Scope{ProjectID: "p", SessionID: "s"}
	store := New(layout)
	payload := []byte(`{"schema_version":3}`)
	if err := store.WriteState(scope, payload); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadState(scope)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("state=%s err=%v", got, err)
	}
	if filepath.Base(store.StatePath(scope)) != "context.json" {
		t.Fatalf("state path=%s", store.StatePath(scope))
	}
	if _, err := os.Stat(store.StatePath(scope)); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointStorageKey(t *testing.T) {
	if CheckpointStorageKey("") != "checkpoint" {
		t.Fatal("empty key changed")
	}
	if CheckpointStorageKey("same") != CheckpointStorageKey("same") {
		t.Fatal("checkpoint key not deterministic")
	}
	if CheckpointStorageKey("a") == CheckpointStorageKey("b") {
		t.Fatal("different checkpoint IDs collided")
	}
}

func TestSessionContextRecordWireIdentity(t *testing.T) {
	record := SessionContextRecord{
		SchemaVersion: SessionContextSchemaVersion,
		SystemPrompt:  "system",
		PlanStack:     []PlanFrame{{PlanID: "p1", Status: "active"}},
		TaskStack:     []TaskFrame{{TaskID: "t1", Status: "active"}},
		SkillStack:    []SkillFrame{{SkillID: "s1", Name: "skill"}},
		CompactStack: []CompactFrame{{
			SegmentID: "c1", From: 1, To: 2, Summary: "sum",
		}},
		GoalStack: []GoalFrame{{GoalID: "g1", Status: "active"}},
		GoalAudit: []GoalAuditEntry{{Seq: 1, Kind: "goal.begin", GoalID: "g1"}},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"schema_version", "system_prompt", "plan_stack", "task_stack",
		"skill_stack", "compact_stack", "goal_stack", "goal_audit",
	} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing context field %s: %s", key, data)
		}
	}
	var decoded SessionContextRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PlanStack[0].PlanID != "p1" || decoded.CompactStack[0].SegmentID != "c1" ||
		decoded.GoalAudit[0].GoalID != "g1" {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestContextPayloadWireIdentity(t *testing.T) {
	checkpoint, err := json.Marshal(CheckpointContent{SessionID: "s", Payload: []byte("x")})
	if err != nil || !strings.Contains(string(checkpoint), `"session_id":"s"`) ||
		!strings.Contains(string(checkpoint), `"payload":"eA=="`) {
		t.Fatalf("checkpoint=%s err=%v", checkpoint, err)
	}
	system, err := json.Marshal(SystemPromptSnapshot{SessionID: "s", Prompt: "p", Version: "v"})
	if err != nil || !strings.Contains(string(system), `"prompt":"p"`) ||
		!strings.Contains(string(system), `"version":"v"`) {
		t.Fatalf("system=%s err=%v", system, err)
	}
}
