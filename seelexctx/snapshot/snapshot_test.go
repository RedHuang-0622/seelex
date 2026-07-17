package snapshot

import (
	"strings"
	"testing"
	"time"
)

func TestFormat_Full(t *testing.T) {
	snap := &ContextSnapshot{
		SourceSessionID: "sess_test",
		ExportedAt:      time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Goal:            "test goal",
		Decisions:       []Decision{{What: "方案A", Why: "性能更好"}},
		Findings:        []string{"重要发现"},
		Progress:        "50%",
		MessageCount:    5,
	}
	out := snap.Format()
	for _, s := range []string{"继承上下文", "test goal", "方案A", "重要发现", "50%"} {
		if !strings.Contains(out, s) {
			t.Fatalf("expected %q in output", s)
		}
	}
}

func TestFormat_Empty(t *testing.T) {
	out := (&ContextSnapshot{}).Format()
	if !strings.Contains(out, "继承上下文") {
		t.Fatal("expected header")
	}
}

func TestFormat_WithEscape(t *testing.T) {
	snap := &ContextSnapshot{
		SourceSessionID: "s",
		ExportedAt:      time.Now(),
		Goal:            "g",
		Escape:          &EscapeInfo{Reason: "done", Message: "ok", Iterations: 3},
	}
	out := snap.Format()
	if !strings.Contains(out, "done") {
		t.Fatal("expected escape reason")
	}
}

func TestBuilderChain(t *testing.T) {
	snap := (&ContextSnapshot{}).
		SetGoal("g").AddDecision("d", "w").AddFinding("f").
		SetProgress("p").AddConstraint("c").AddPendingWork("w").
		SetEscape("done", "ok", 1)
	if snap.Goal != "g" || len(snap.Decisions) != 1 || len(snap.Findings) != 1 ||
		snap.Progress != "p" || len(snap.Constraints) != 1 || len(snap.PendingWork) != 1 {
		t.Fatal("builder chain failed")
	}
}

func TestValidate_OK(t *testing.T) {
	err := (&ContextSnapshot{SourceSessionID: "s", ExportedAt: time.Now(), Goal: "g"}).Validate()
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_Nil(t *testing.T) {
	var snap *ContextSnapshot
	if err := snap.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_NoGoal(t *testing.T) {
	err := (&ContextSnapshot{SourceSessionID: "s", ExportedAt: time.Now()}).Validate()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_GoalFromParent(t *testing.T) {
	err := (&ContextSnapshot{
		SourceSessionID: "s", ExportedAt: time.Now(),
		Escape: &EscapeInfo{ParentGoal: "parent"},
	}).Validate()
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidationError(t *testing.T) {
	ve := &ValidationError{Field: "f", Err: "bad"}
	if !strings.Contains(ve.Error(), "f") {
		t.Fatal("unexpected error string")
	}
}

func TestTruncate(t *testing.T) {
	if r := Truncate("hello", 10); r != "hello" {
		t.Fatalf("got %q", r)
	}
	if r := Truncate("hello world", 8); len(r) != 8 {
		t.Fatalf("got %q len=%d", r, len(r))
	}
}

func TestSetParentGoal(t *testing.T) {
	snap := (&ContextSnapshot{}).SetEscape("done", "ok", 1).SetParentGoal("parent")
	if snap.Escape.ParentGoal != "parent" {
		t.Fatal("parent goal not set")
	}
}

func TestSetParentGoalNoEscape(t *testing.T) {
	// should not panic
	(&ContextSnapshot{}).SetParentGoal("parent")
}
