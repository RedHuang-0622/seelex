package compactor

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

func largeSnap() *snapshot.ContextSnapshot {
	s := &snapshot.ContextSnapshot{
		Goal:     "大型集成测试目标：验证所有模块的正确性兼容性和性能表现",
		Progress: "已完成初步调研和架构设计，正在进行核心模块开发和单元测试编写",
	}
	for i := 0; i < 15; i++ {
		s.Decisions = append(s.Decisions, snapshot.Decision{
			What: fmt.Sprintf("decision item number %d about the system architecture design", i),
			Why:  fmt.Sprintf("because decision %d provides better scalability", i),
		})
	}
	for i := 0; i < 40; i++ {
		s.Findings = append(s.Findings, fmt.Sprintf("important finding number %d discovered during investigation", i))
	}
	for i := 0; i < 10; i++ {
		s.Constraints = append(s.Constraints, fmt.Sprintf("constraint %d: handle %d concurrent users", i, i*100))
	}
	for i := 0; i < 8; i++ {
		s.PendingWork = append(s.PendingWork, fmt.Sprintf("implement feature module %d", i))
	}
	return s
}

func TestCompact_Nil(t *testing.T) {
	_, err := NewCompactor().Compact(nil, 1000)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompact_FullBudget(t *testing.T) {
	snap := &snapshot.ContextSnapshot{
		Goal: "test", Progress: "p", Findings: []string{"f1", "f2"},
		Decisions: []snapshot.Decision{{What: "d1", Why: "w1"}},
	}
	r, err := NewCompactor().Compact(snap, 500)
	if err != nil {
		t.Fatal(err)
	}
	if r == snap {
		t.Fatal("should be copy")
	}
	if len(r.Findings) != 2 {
		t.Fatalf("got %d", len(r.Findings))
	}
}

func TestCompact_Summary(t *testing.T) {
	snap := largeSnap()
	r, err := NewCompactor().Compact(snap, 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Decisions) != 1 {
		t.Fatalf("expected 1 summary decision, got %d", len(r.Decisions))
	}
	if len(r.Findings) != 1 {
		t.Fatalf("expected 1 summary finding, got %d", len(r.Findings))
	}
	if r.Goal == "" {
		t.Fatal("summary must retain the goal when the budget permits")
	}
}

func TestCompact_Minimal(t *testing.T) {
	snap := largeSnap()
	r, err := NewCompactor().Compact(snap, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 0 {
		t.Fatal("expected 0 findings in minimal")
	}
	if len(r.Decisions) != 0 {
		t.Fatal("expected 0 decisions in minimal")
	}
}

func TestCompact_NegativeBudget(t *testing.T) {
	_, err := NewCompactor().Compact(&snapshot.ContextSnapshot{Goal: "t"}, -1)
	if !errors.Is(err, ErrBudgetTooSmall) {
		t.Fatalf("error = %v, want ErrBudgetTooSmall", err)
	}
}

func TestEstimateTokens(t *testing.T) {
	snap := &snapshot.ContextSnapshot{Goal: "test", Progress: "p"}
	snap.Decisions = []snapshot.Decision{{What: "d", Why: "w"}}
	snap.Findings = []string{"f1"}
	snap.Escape = &snapshot.EscapeInfo{Reason: "done", Message: "ok"}
	n := estimateTokens(snap)
	if n <= 0 {
		t.Fatalf("got %d", n)
	}
}

func TestTruncateForToken(t *testing.T) {
	if r := truncateForToken("short", 100); r != "short" {
		t.Fatalf("got %q", r)
	}
	r := truncateForToken(strings.Repeat("a", 500), 5)
	if len(r) >= 500 {
		t.Fatal("expected truncation")
	}
}

func TestTruncateForTokenKeepsUTF8AndBudget(t *testing.T) {
	input := strings.Repeat("中文🙂", 100)
	truncated := truncateForToken(input, 25)
	if !utf8.ValidString(truncated) {
		t.Fatalf("invalid UTF-8 result: %q", truncated)
	}
	if got := seelectx.EstimateTokens(truncated); got > 25 {
		t.Fatalf("token estimate = %d, want <= 25", got)
	}
}

func TestCompactNeverExceedsBudget(t *testing.T) {
	snap := largeSnap()
	snap.Goal = strings.Repeat("目标🙂", 200)
	snap.Progress = strings.Repeat("进度🙂", 200)
	snap.Escape = &snapshot.EscapeInfo{Reason: strings.Repeat("错误", 80), Message: strings.Repeat("详情", 80)}

	for _, budget := range []int{500, 250, 100, 40} {
		compacted, err := NewCompactor().Compact(snap, budget)
		if err != nil {
			t.Fatalf("budget %d: %v", budget, err)
		}
		if got := estimateTokens(compacted); got > budget {
			t.Fatalf("budget %d: compacted token estimate = %d", budget, got)
		}
	}
}

func TestCompact_SummaryToolCalls(t *testing.T) {
	snap := &snapshot.ContextSnapshot{
		Goal: "test with tool calls and enough data to trigger summary compression mode",
		Decisions: []snapshot.Decision{
			{What: "调用工具 read_file", Why: "reading file content for analysis"},
			{What: "调用工具 grep", Why: "search for pattern in source code"},
			{What: "something else", Why: "other non-tool decision here"},
		},
	}
	for i := 0; i < 40; i++ {
		snap.Findings = append(snap.Findings, fmt.Sprintf("finding number %d with enough text to increase token count", i))
	}
	r, err := NewCompactor().Compact(snap, 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Decisions) != 1 {
		t.Fatalf("expected 1, got %d", len(r.Decisions))
	}
}
