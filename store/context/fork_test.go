package context

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestForkStateCreatesEmptyChild(t *testing.T) {
	forkedFrom := json.RawMessage(`{"parent":"p1","cut":7}`)
	payload, frames, err := ForkState(nil, ForkOptions{ForkedFrom: forkedFrom})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("empty child frames=%+v", frames)
	}
	var record SessionContextRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatal(err)
	}
	if record.SchemaVersion != SessionContextSchemaVersion || string(record.ForkedFrom) != string(forkedFrom) {
		t.Fatalf("child record=%+v", record)
	}
}

func TestForkStateFiltersSkillsAndRewritesCompactFrames(t *testing.T) {
	cutTime := time.Unix(100, 0)
	parent := SessionContextRecord{
		SchemaVersion: SessionContextSchemaVersion,
		PlanStack:     []PlanFrame{{PlanID: "plan"}},
		TaskStack:     []TaskFrame{{TaskID: "task"}},
		GoalStack:     []GoalFrame{{GoalID: "goal"}},
		GoalAudit:     []GoalAuditEntry{{GoalID: "goal", Kind: "goal.begin"}},
		SkillStack: []SkillFrame{
			{SkillID: "before", ActivatedAt: cutTime},
			{SkillID: "after", ActivatedAt: cutTime.Add(time.Second)},
		},
		CompactStack: []CompactFrame{
			{SegmentID: "legacy"},
			{SegmentID: "before", EventFrom: 1, EventTo: 4},
			{SegmentID: "cross", EventFrom: 3, EventTo: 9, MessageTo: "old"},
			{SegmentID: "after", EventFrom: 8, EventTo: 12},
		},
	}
	parentPayload, err := json.Marshal(parent)
	if err != nil {
		t.Fatal(err)
	}

	childPayload, frames, err := ForkState(parentPayload, ForkOptions{
		ForkedFrom:   json.RawMessage(`{"parent":"p1"}`),
		CutEventSeq:  7,
		CutTime:      cutTime,
		CutMessageID: "cut-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 || frames[2].SegmentID != "cross" ||
		frames[2].EventTo != 7 || frames[2].MessageTo != "cut-message" {
		t.Fatalf("frames=%+v", frames)
	}
	var child SessionContextRecord
	if err := json.Unmarshal(childPayload, &child); err != nil {
		t.Fatal(err)
	}
	if len(child.PlanStack) != 0 || len(child.TaskStack) != 0 ||
		len(child.GoalStack) != 0 || len(child.GoalAudit) != 0 {
		t.Fatalf("non-inherited stacks=%+v", child)
	}
	if len(child.SkillStack) != 1 || child.SkillStack[0].SkillID != "before" {
		t.Fatalf("skills=%+v", child.SkillStack)
	}
}

func TestForkStateRejectsCorruptOrUnsupportedParent(t *testing.T) {
	if _, _, err := ForkState([]byte("not-json"), ForkOptions{}); err == nil ||
		!strings.Contains(err.Error(), "decode parent context") {
		t.Fatalf("corrupt error=%v", err)
	}
	payload := []byte(`{"schema_version":99}`)
	if _, _, err := ForkState(payload, ForkOptions{}); err == nil ||
		!strings.Contains(err.Error(), "unsupported schema version 99") {
		t.Fatalf("schema error=%v", err)
	}
}
