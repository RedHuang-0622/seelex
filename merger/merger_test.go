package merger

import (
	"testing"

	"github.com/RedHuang-0622/seelex/snapshot"
)

func TestMergeBack_NilParent(t *testing.T) {
	if err := NewMerger().MergeBack(nil, &snapshot.ContextSnapshot{}); err == nil { t.Fatal("expected error") }
}

func TestMergeBack_NilChild(t *testing.T) {
	if err := NewMerger().MergeBack(&snapshot.ContextSnapshot{}, nil); err == nil { t.Fatal("expected error") }
}

func TestMergeBack_FindingsAppend(t *testing.T) {
	p := &snapshot.ContextSnapshot{Findings: []string{"p1"}}
	c := &snapshot.ContextSnapshot{Findings: []string{"c1"}}
	if err := NewMerger().MergeBack(p, c); err != nil { t.Fatal(err) }
	if len(p.Findings) != 2 { t.Fatalf("got %d", len(p.Findings)) }
}

func TestMergeBack_DecisionsAppend(t *testing.T) {
	p := &snapshot.ContextSnapshot{Decisions: []snapshot.Decision{{What: "p", Why: "pw"}}}
	c := &snapshot.ContextSnapshot{Decisions: []snapshot.Decision{{What: "c", Why: "cw"}}}
	if err := NewMerger().MergeBack(p, c); err != nil { t.Fatal(err) }
	if len(p.Decisions) != 2 { t.Fatalf("got %d", len(p.Decisions)) }
}

func TestMergeBack_ProgressReplace(t *testing.T) {
	p := &snapshot.ContextSnapshot{Progress: "old"}
	c := &snapshot.ContextSnapshot{Progress: "new"}
	if err := NewMerger().MergeBack(p, c); err != nil { t.Fatal(err) }
	if p.Progress != "new" { t.Fatalf("got %q", p.Progress) }
}

func TestMergeBack_ProgressKeep(t *testing.T) {
	p := &snapshot.ContextSnapshot{Progress: "keep"}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{}); err != nil { t.Fatal(err) }
	if p.Progress != "keep" { t.Fatalf("got %q", p.Progress) }
}

func TestMergeBack_PendingReplace(t *testing.T) {
	p := &snapshot.ContextSnapshot{PendingWork: []string{"old"}}
	c := &snapshot.ContextSnapshot{PendingWork: []string{"n1", "n2"}}
	if err := NewMerger().MergeBack(p, c); err != nil { t.Fatal(err) }
	if len(p.PendingWork) != 2 { t.Fatalf("got %d", len(p.PendingWork)) }
}

func TestMergeBack_TokenSum(t *testing.T) {
	p := &snapshot.ContextSnapshot{TokenEstimate: 100}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{TokenEstimate: 50}); err != nil { t.Fatal(err) }
	if p.TokenEstimate != 150 { t.Fatalf("got %d", p.TokenEstimate) }
}

func TestMergeBack_MessageCountSum(t *testing.T) {
	p := &snapshot.ContextSnapshot{MessageCount: 5}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{MessageCount: 3}); err != nil { t.Fatal(err) }
	if p.MessageCount != 8 { t.Fatalf("got %d", p.MessageCount) }
}

func TestMergeBack_EscapeReplace(t *testing.T) {
	p := &snapshot.ContextSnapshot{Escape: &snapshot.EscapeInfo{Reason: "old"}}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{Escape: &snapshot.EscapeInfo{Reason: "new"}}); err != nil { t.Fatal(err) }
	if p.Escape.Reason != "new" { t.Fatalf("got %q", p.Escape.Reason) }
}

func TestMergeBack_ConstraintsDedup(t *testing.T) {
	p := &snapshot.ContextSnapshot{Constraints: []string{"c1", "c2"}}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{Constraints: []string{"c2", "c3"}}); err != nil { t.Fatal(err) }
	if len(p.Constraints) != 3 { t.Fatalf("got %d: %v", len(p.Constraints), p.Constraints) }
}

func TestMergeBack_CopyOnWrite(t *testing.T) {
	orig := []snapshot.Decision{{What: "orig", Why: "w"}}
	p := &snapshot.ContextSnapshot{Decisions: orig}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{Decisions: []snapshot.Decision{{What: "c", Why: "w"}}}); err != nil { t.Fatal(err) }
	if len(orig) != 1 { t.Fatal("original should be unmodified") }
}

func TestMergeBack_Alternatives(t *testing.T) {
	p := &snapshot.ContextSnapshot{
		Decisions: []snapshot.Decision{{What: "p", Why: "w", Alternatives: []string{"a1", "a2"}}},
	}
	if err := NewMerger().MergeBack(p, &snapshot.ContextSnapshot{
		Decisions: []snapshot.Decision{{What: "c", Why: "w", Alternatives: []string{"a3"}}},
	}); err != nil { t.Fatal(err) }
	if len(p.Decisions[0].Alternatives) != 2 { t.Fatal("alternatives should be preserved") }
	if len(p.Decisions[1].Alternatives) != 1 { t.Fatal("child alternatives not copied") }
}
