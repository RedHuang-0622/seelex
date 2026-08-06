package memory

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

func candidates(entries ...Candidate) []Candidate {
	return entries
}

func TestSelectMatchesSummaryTerms(t *testing.T) {
	entries := candidates(
		Candidate{SegmentID: "compact-a-1", Summary: "修复了权限 gate 的实现，含 approve 流程", From: 0, To: 5},
		Candidate{SegmentID: "compact-a-2", Summary: "完成 MCP server 冷启动与 mcp_load 接线", From: 6, To: 12},
		Candidate{SegmentID: "compact-a-3", Summary: "GUI 构建 ldflags 与 tags 接线", From: 13, To: 20},
	)
	selected := Select("权限 approve 如何实现", entries, DefaultOptions())
	if len(selected) != 1 {
		t.Fatalf("want 1 hit, got %d", len(selected))
	}
	if selected[0].SegmentID != "compact-a-1" {
		t.Fatalf("want compact-a-1, got %s", selected[0].SegmentID)
	}
}

func TestSelectASCIIAndCJKBigramTokenization(t *testing.T) {
	// 中文按 bigram："压缩"命中第一条；"mcp_load" ASCII 词命中第二条。
	entries := candidates(
		Candidate{SegmentID: "compact-a-1", Summary: "窗口外压缩与栈帧合并", From: 0, To: 5},
		Candidate{SegmentID: "compact-a-2", Summary: "mcp_load 冷启动接入 runtime", From: 6, To: 12},
	)
	selected := Select("压缩上下文如何工作", entries, DefaultOptions())
	if len(selected) != 1 || selected[0].SegmentID != "compact-a-1" {
		t.Fatalf("want compact-a-1 for CJK query, got %+v", selected)
	}
	selected = Select("mcp_load 接线细节", entries, DefaultOptions())
	if len(selected) != 1 || selected[0].SegmentID != "compact-a-2" {
		t.Fatalf("want compact-a-2 for ASCII query, got %+v", selected)
	}
}

func TestSelectLimitAndRecency(t *testing.T) {
	entries := candidates(
		Candidate{SegmentID: "compact-a-1", Summary: "权限 gate 设计与实现", From: 0, To: 5},
		Candidate{SegmentID: "compact-a-2", Summary: "权限 gate 测试补充", From: 6, To: 12},
		Candidate{SegmentID: "compact-a-3", Summary: "权限 gate 文档更新", From: 13, To: 20},
	)
	selected := Select("权限 gate", entries, Options{Limit: 2})
	if len(selected) != 2 {
		t.Fatalf("want 2 hits, got %d", len(selected))
	}
	// 同分时 recency 加分：越新的段越靠前。
	if selected[0].SegmentID != "compact-a-3" || selected[1].SegmentID != "compact-a-2" {
		t.Fatalf("want newest first [a-3 a-2], got [%s %s]", selected[0].SegmentID, selected[1].SegmentID)
	}
}

func TestSelectFiltersByEvidenceRefs(t *testing.T) {
	entries := candidates(
		Candidate{SegmentID: "compact-a-1", Summary: "上下文栈持久化", Evidence: []sessionstore.EvidenceRef{{Ref: "result:abc", Summary: "permission gate test output"}}},
		Candidate{SegmentID: "compact-a-2", Summary: "MCP 服务器刷新", Evidence: []sessionstore.EvidenceRef{{Ref: "result:def", Summary: "mcp attach result"}}},
	)
	// 查询命中证据摘要而不是正文。
	selected := Select("permission", entries, DefaultOptions())
	if len(selected) != 1 || selected[0].SegmentID != "compact-a-1" {
		t.Fatalf("want evidence-hit compact-a-1, got %+v", selected)
	}
}

func TestSelectEmptyQueryAndNoHit(t *testing.T) {
	entries := candidates(Candidate{SegmentID: "compact-a-1", Summary: "权限 gate 实现"})
	if got := Select("", entries, DefaultOptions()); got != nil {
		t.Fatalf("empty query must return nil, got %+v", got)
	}
	if got := Select("   ", entries, DefaultOptions()); got != nil {
		t.Fatalf("whitespace query must return nil, got %+v", got)
	}
	// 单字 CJK 查询无可选词项（不构成 bigram）。
	if got := Select("权", entries, DefaultOptions()); got != nil {
		t.Fatalf("single-rune CJK query has no terms, got %+v", got)
	}
	if got := Select("postgresql", entries, DefaultOptions()); got != nil {
		t.Fatalf("no-hit query must return nil, got %+v", got)
	}
}

func TestSelectMinScoreFiltersWeakHits(t *testing.T) {
	entries := candidates(
		Candidate{SegmentID: "compact-a-1", Summary: "权限 gate 设计与实现细节", From: 0, To: 5},
		Candidate{SegmentID: "compact-a-2", Summary: "权限 gate", From: 6, To: 12},
	)
	// min_score 高于弱命中（raw=2.0，仅两个词项命中）→ 只留强命中（raw=3.0）。
	selected := Select("权限 gate 设计", entries, Options{MinScore: 2.5})
	if len(selected) != 1 || selected[0].SegmentID != "compact-a-1" {
		t.Fatalf("want strong hit only, got %+v", selected)
	}
}

func TestSelectEmptyCandidates(t *testing.T) {
	if got := Select("权限", nil, DefaultOptions()); got != nil {
		t.Fatalf("nil candidates must return nil, got %+v", got)
	}
	if got := Select("权限", []Candidate{}, DefaultOptions()); got != nil {
		t.Fatalf("empty candidates must return nil, got %+v", got)
	}
}

func TestRenderMemoryBlock(t *testing.T) {
	selected := candidates(
		Candidate{SegmentID: "compact-a-1", Summary: "权限 gate 设计与实现细节", From: 0, To: 5, Evidence: []sessionstore.EvidenceRef{{Ref: "result:abc"}}},
	)
	block := RenderMemoryBlock(selected, 1024)
	if block == nil {
		t.Fatal("block must render for hits")
	}
	content := block.Messages[0]
	if content.Content == nil {
		t.Fatal("block content missing")
	}
	for _, want := range []string{"## 相关记忆", "compact-a-1", "[0..5]", "权限 gate 设计与实现细节", "result:abc", "不作为事实"} {
		if !strings.Contains(*content.Content, want) {
			t.Fatalf("block must contain %q, got:\n%s", want, *content.Content)
		}
	}
	if block.Name != "memory" {
		t.Fatalf("block name must be memory, got %s", block.Name)
	}
}

func TestRenderMemoryBlockBoundsAndNil(t *testing.T) {
	if got := RenderMemoryBlock(nil, 1024); got != nil {
		t.Fatalf("no hits must render nil, got %+v", got)
	}
	long := strings.Repeat("权限 gate 设计与实现细节与证据 ", 200)
	selected := candidates(Candidate{SegmentID: "compact-a-1", Summary: long, From: 0, To: 5})
	block := RenderMemoryBlock(selected, 64)
	if block == nil {
		t.Fatal("block must render")
	}
	content := *block.Messages[0].Content
	if tokens := seelectx.EstimateTokens(content); tokens > 80 {
		t.Fatalf("block must respect token budget (est=%d > 64*1.25)", tokens)
	}
	if !strings.HasSuffix(content, "…\n") && !strings.Contains(content, "…") {
		t.Fatalf("truncated summary must carry ellipsis, got:\n%s", content)
	}
}

func TestRenderMemoryBlockEmptySummary(t *testing.T) {
	block := RenderMemoryBlock(candidates(Candidate{SegmentID: "compact-a-1", From: 0, To: 5}), 64)
	if block == nil {
		t.Fatal("block must render")
	}
	if !strings.Contains(*block.Messages[0].Content, "(无摘要)") {
		t.Fatalf("empty summary must be marked, got:\n%s", *block.Messages[0].Content)
	}
}
