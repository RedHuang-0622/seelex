package history

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestAssemblerMatchesOriginalWindowFunctions(t *testing.T) {
	page := []model.Message{
		row("a", "user", "a"),
		row("b", "assistant", "b"),
	}
	memory := []model.Message{
		row("b", "assistant", "b"),
		row("c", "user", "c"),
	}
	var assembler Assembler

	wantRows, wantStart := ReplaceTail(page, memory, 1, 4, 10)
	gotRows, gotStart := assembler.ReplaceTail(page, memory, 1, 4, 10)
	if !reflect.DeepEqual(gotRows, wantRows) || gotStart != wantStart {
		t.Fatalf("ReplaceTail mismatch: got (%v,%d), want (%v,%d)", ids(gotRows), gotStart, ids(wantRows), wantStart)
	}

	wantRows, wantStart = Prepend(page, memory, 4, 10)
	gotRows, gotStart = assembler.Prepend(page, memory, 4, 10)
	if !reflect.DeepEqual(gotRows, wantRows) || gotStart != wantStart {
		t.Fatalf("Prepend mismatch: got (%v,%d), want (%v,%d)", ids(gotRows), gotStart, ids(wantRows), wantStart)
	}

	withSystem := append([]model.Message{row("s", "system", "prompt")}, page...)
	if !reflect.DeepEqual(assembler.DurableRows(withSystem), DurableRows(withSystem)) {
		t.Fatal("DurableRows mismatch")
	}
	want := model.Message{Role: "tool", Tool: &model.ToolCall{ID: "call-1"}}
	rows := []model.Message{{Role: "tool", Tool: &model.ToolCall{ID: "call-1"}}}
	if assembler.IndexOfSameRow(rows, want) != IndexOfSameRow(rows, want) {
		t.Fatal("IndexOfSameRow mismatch")
	}
	if assembler.ContainsSameRow(rows, want) != ContainsSameRow(rows, want) {
		t.Fatal("ContainsSameRow mismatch")
	}
	if assembler.HasRowIdentity(model.Message{}) != HasRowIdentity(model.Message{}) {
		t.Fatal("HasRowIdentity mismatch")
	}
}

func TestAssemblerKeepsWindowOnUnmatchedSeamAndIgnoresEmptyPlaceholders(t *testing.T) {
	var assembler Assembler
	page := []model.Message{row("x", "user", "x")}
	memory := []model.Message{row("a", "user", "a")}
	rows, start := assembler.Prepend(page, memory, 3, 10)
	if rows != nil || start != 3 {
		t.Fatalf("unmatched seam must keep window: rows=%v start=%d", ids(rows), start)
	}
	if assembler.HasRowIdentity(model.Message{}) {
		t.Fatal("empty placeholder must not carry row identity")
	}
}
