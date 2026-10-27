package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
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

func TestStackWireAndVocabulary(t *testing.T) {
	if !ValidStackKind(StackKindPlan) || !ValidStackKind(StackKindTask) ||
		!ValidStackKind(StackKindGoal) || !ValidStackKind(StackKindSubagent) ||
		ValidStackKind("bad") {
		t.Fatal("stack kind vocabulary mismatch")
	}
	for _, status := range []string{
		StackStatusPlanClosed, StackStatusTaskCompleted, StackStatusTaskFailed,
		StackStatusGoalAborted, StackStatusSubagentDone, StackStatusArchived,
	} {
		if !IsTerminalStackStatus(status) {
			t.Fatalf("status %q should be terminal", status)
		}
	}
	if IsTerminalStackStatus("active") {
		t.Fatal("active must not be terminal")
	}
	record := StackItemRecord{
		ItemID: "i1", BatchID: "b1", StackID: "plan|active",
		Kind: StackKindPlan, Seq: 1, Status: "active", Revision: 2,
		Payload: json.RawMessage(`{"id":"i1"}`),
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"item_id", "batch_id", "stack_id", "kind", "seq", "payload", "revision"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("missing stack field %s: %s", key, data)
		}
	}
}

func TestAppendCompactChain(t *testing.T) {
	first := CompactFrame{SegmentID: "c1", From: 0, To: 1}
	stack, err := AppendCompact(nil, first)
	if err != nil || len(stack) != 1 {
		t.Fatalf("first=%v err=%v", stack, err)
	}
	if _, err := AppendCompact(stack, CompactFrame{SegmentID: "c2"}); err == nil {
		t.Fatal("non-first frame without prev must fail")
	}
	second := CompactFrame{SegmentID: "c2", From: 0, To: 2, PrevSegmentID: "c1"}
	stack, err = AppendCompact(stack, second)
	if err != nil || len(stack) != 2 {
		t.Fatalf("second=%v err=%v", stack, err)
	}
	if _, err := AppendCompact(stack, CompactFrame{SegmentID: "c3", From: 2, To: 1, PrevSegmentID: "c2"}); err == nil {
		t.Fatal("invalid range must fail")
	}
}

func TestWireParamsAndResultWire(t *testing.T) {
	params := NormalizeWireParams(WireParams{})
	if params.K != 3 || params.Budget != DefaultWireBudget || !params.Repair {
		t.Fatalf("normalized=%+v", params)
	}
	budget := ApplyWireBudget(storeroot.DefaultSettings(), WireParams{Budget: 100_000})
	if budget.Budget <= 0 || budget.Budget >= 100_000 {
		t.Fatalf("soft budget=%d", budget.Budget)
	}
	result := WireResult{Messages: []WireMessage{{Role: WireRoleUser, Content: "hi"}}, TailStartSeq: 1}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"messages", "need_compact", "prefix_digest", "tail_start_seq"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("missing wire field %s: %s", key, data)
		}
	}
}

func TestContextOperationValidation(t *testing.T) {
	now := time.Unix(1, 0)
	if _, err := NewPlanStackInput(PlanFrame{}, now); err == nil {
		t.Fatal("empty plan id must fail")
	}
	plan, err := NewPlanStackInput(PlanFrame{PlanID: "p1"}, now)
	if err != nil || plan.Status != "active" || plan.EnteredAt.IsZero() {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	task, err := NewTaskStackInput(TaskFrame{TaskID: "t1", Status: "queued"}, now)
	if err != nil || task.Status != "queued" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	goal, err := NewGoalStackInput(GoalFrame{GoalID: "g1", RoleName: "tl"}, now)
	if err != nil || goal.RoleName != "tl" {
		t.Fatalf("goal=%+v err=%v", goal, err)
	}
	if err := RequireTopGoal([]GoalFrame{{GoalID: "g0"}, {GoalID: "g1"}}, "g0"); err == nil {
		t.Fatal("non-top goal must fail")
	}
	skills, err := PushSkillFrame(nil, SkillFrame{SkillID: "s1"}, now)
	if err != nil || len(skills) != 1 || skills[0].ActivatedAt.IsZero() {
		t.Fatalf("skills=%+v err=%v", skills, err)
	}
	skills, err = PopSkillFrame(skills, "s1")
	if err != nil || len(skills) != 0 {
		t.Fatalf("pop skills=%+v err=%v", skills, err)
	}
}
