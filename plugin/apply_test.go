package plugin

import (
	"errors"
	"strings"
	"testing"
)

// TestTransactionRollsBackInReverseOrder 验证失败时按逆序执行已成功步骤的
// Undo（正向顺序 1→2→3，失败于 3；回滚顺序 2→1）。
func TestTransactionRollsBackInReverseOrder(t *testing.T) {
	var order []string
	err := Transaction(
		Step{Name: "one", Do: func() error { order = append(order, "do:1"); return nil }, Undo: func() error { order = append(order, "undo:1"); return nil }},
		Step{Name: "two", Do: func() error { order = append(order, "do:2"); return nil }, Undo: func() error { order = append(order, "undo:2"); return nil }},
		Step{Name: "three", Do: func() error { order = append(order, "do:3"); return errors.New("boom") }, Undo: func() error { order = append(order, "undo:3"); return nil }},
	)
	if err == nil || !strings.Contains(err.Error(), "three: boom") {
		t.Fatalf("err = %v, want step-three failure", err)
	}
	want := []string{"do:1", "do:2", "do:3", "undo:2", "undo:1"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestTransactionJoinsUndoErrors 验证回滚错误与正向错误聚合返回。
func TestTransactionJoinsUndoErrors(t *testing.T) {
	err := Transaction(
		Step{Name: "one", Do: func() error { return nil }, Undo: func() error { return errors.New("undo failed") }},
		Step{Name: "two", Do: func() error { return errors.New("do failed") }},
	)
	if err == nil {
		t.Fatal("expected joined error")
	}
	if !strings.Contains(err.Error(), "do failed") || !strings.Contains(err.Error(), "undo failed") {
		t.Fatalf("err = %v, want both do and undo failures", err)
	}
}

// TestTransactionSkipsNilUndo 验证 nil Undo 步骤在回滚中被跳过。
func TestTransactionSkipsNilUndo(t *testing.T) {
	var undoRuns int
	err := Transaction(
		Step{Name: "one", Do: func() error { return nil }},
		Step{Name: "two", Do: func() error { return errors.New("boom") }, Undo: func() error { undoRuns++; return nil }},
	)
	if err == nil || undoRuns != 0 {
		t.Fatalf("err=%v undoRuns=%d, want err non-nil and no undo", err, undoRuns)
	}
}

// TestDiffState 验证新增/删除/修改三类差异归类正确。
func TestDiffState(t *testing.T) {
	current := map[string]Plugin{
		"keep":   {Name: "keep"},
		"change": {Name: "change", Description: "old"},
		"drop":   {Name: "drop"},
	}
	next := map[string]Plugin{
		"keep":   {Name: "keep"},
		"change": {Name: "change", Description: "new"},
		"add":    {Name: "add"},
	}
	diff := DiffState(current, next)
	if len(diff.Added) != 1 || diff.Added[0] != "add" {
		t.Fatalf("Added = %v, want [add]", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "drop" {
		t.Fatalf("Removed = %v, want [drop]", diff.Removed)
	}
	if len(diff.Updated) != 1 || diff.Updated[0] != "change" {
		t.Fatalf("Updated = %v, want [change]", diff.Updated)
	}
}
