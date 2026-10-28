package residency

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// 本文件原在 `application/assembly/session_input_index_test.go`，随判据（M5-R6-input-index）一起归位；
// 留在 core 的是 Service 级用例（全量覆盖未加载轮次、空会话、缺 ID、record 回退、内部行过滤）。

func TestSummarizeInputText(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		limit     int
		wantSum   string
		wantChars int
	}{
		{name: "short", text: "  你好   世界\n", limit: 10, wantSum: "你好 世界", wantChars: 5},
		{name: "exact limit", text: strings.Repeat("a", 10), limit: 10, wantSum: strings.Repeat("a", 10), wantChars: 10},
		{name: "over limit", text: strings.Repeat("a", 11), limit: 10, wantSum: strings.Repeat("a", 10) + "…", wantChars: 11},
		{name: "default limit", text: strings.Repeat("b", 200), limit: 0,
			wantSum: strings.Repeat("b", InputIndexSummaryLimit) + "…", wantChars: 200},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			summary, chars := SummarizeInputText(testCase.text, testCase.limit)
			if summary != testCase.wantSum || chars != testCase.wantChars {
				t.Fatalf("summarize(%q, %d) = %q/%d, want %q/%d",
					testCase.text, testCase.limit, summary, chars, testCase.wantSum, testCase.wantChars)
			}
		})
	}
}

// TestAlignLoadedInputTexts 对齐算法：尾部窗口整段命中；最新一条输入尚未进入索引时退一步用更短前缀；
// 完全对不上时不标（宁可不标也不错位）。
func TestAlignLoadedInputTexts(t *testing.T) {
	texts := []string{"q1", "q2", "q3", "q4", "q5"}
	cases := []struct {
		name       string
		loaded     []string
		wantStart  int
		wantLength int
	}{
		{name: "tail window", loaded: []string{"q4", "q5"}, wantStart: 3, wantLength: 2},
		{name: "whole session", loaded: texts, wantStart: 0, wantLength: 5},
		{name: "newest not indexed yet", loaded: []string{"q3", "q4", "q5", "q6"}, wantStart: 2, wantLength: 3},
		{name: "single earliest", loaded: []string{"q1"}, wantStart: 0, wantLength: 1},
		{name: "no match", loaded: []string{"zz"}, wantStart: 0, wantLength: 0},
		{name: "longer than index", loaded: []string{"q1", "q2", "q3", "q4", "q5", "q6", "q7"}, wantStart: 0, wantLength: 0},
		{name: "empty window", loaded: nil, wantStart: 0, wantLength: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			start, length := AlignLoadedInputTexts(texts, testCase.loaded)
			if start != testCase.wantStart || length != testCase.wantLength {
				t.Fatalf("align(%v) = %d/%d, want %d/%d", testCase.loaded, start, length, testCase.wantStart, testCase.wantLength)
			}
		})
	}
}

// TestBuildInputIndexMarksLoadedSection 纯函数口径：只有命中已加载段的输入被标成 Loaded，
// 且序号/偏移/摘要随行保留。
func TestBuildInputIndexMarksLoadedSection(t *testing.T) {
	conversation := []model.Message{
		{ID: "m1", Role: "user", Content: "第一问"},
		{ID: "m2", Role: "assistant", Content: "答一"},
		{ID: "m3", Role: "user", Content: "第二问"},
		{ID: "m4", Role: "user", Content: "第三问"},
	}
	userText := func(message model.Message) string {
		if message.Role != "user" {
			return ""
		}
		return strings.TrimSpace(message.Content)
	}
	rows := BuildInputIndex(conversation, []string{"第二问", "第三问"}, InputIndexSummaryLimit, userText)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3", rows)
	}
	if rows[0].Loaded || !rows[1].Loaded || !rows[2].Loaded {
		t.Fatalf("loaded flags = %v/%v/%v, want false/true/true", rows[0].Loaded, rows[1].Loaded, rows[2].Loaded)
	}
	if rows[1].Offset != 2 || rows[2].Offset != 3 {
		t.Fatalf("offsets = %d/%d, want 2/3", rows[1].Offset, rows[2].Offset)
	}
	if rows[0].Round != 1 || rows[2].Round != 3 {
		t.Fatalf("rounds = %d/%d, want 1/3（序号按保留行计）", rows[0].Round, rows[2].Round)
	}
}

// TestBuildInputIndexUsesLastAssistantReplyPerInput 每个用户刻度只记录下一条
// 用户输入之前的最后一条非空 assistant 回复；空回复不覆盖已有摘要。
func TestBuildInputIndexUsesLastAssistantReplyPerInput(t *testing.T) {
	conversation := []model.Message{
		{ID: "m1", Role: "user", Content: "第一问"},
		{ID: "m2", Role: "assistant", Content: "第一版回答"},
		{ID: "m3", Role: "assistant", Content: "最终回答"},
		{ID: "m4", Role: "user", Content: "第二问"},
		{ID: "m5", Role: "assistant", Content: "  \n "},
		{ID: "m6", Role: "tool", Content: "工具结果"},
		{ID: "m7", Role: "user", Content: "第三问"},
		{ID: "m8", Role: "assistant", Content: strings.Repeat("答", InputIndexReplySummaryLimit+5)},
	}
	userText := func(message model.Message) string {
		if message.Role != "user" {
			return ""
		}
		return strings.TrimSpace(message.Content)
	}
	rows := BuildInputIndex(conversation, nil, InputIndexSummaryLimit, userText)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3", rows)
	}
	if rows[0].ReplySummary != "最终回答" {
		t.Fatalf("first reply summary = %q, want 最终回答", rows[0].ReplySummary)
	}
	if rows[1].ReplySummary != "" || rows[1].ReplyChars != 0 {
		t.Fatalf("second reply = %q/%d, want empty（空 assistant 不得伪造回复）", rows[1].ReplySummary, rows[1].ReplyChars)
	}
	if got := []rune(rows[2].ReplySummary); len(got) != InputIndexReplySummaryLimit+1 ||
		got[len(got)-1] != '…' || rows[2].ReplyChars != InputIndexReplySummaryLimit+5 {
		t.Fatalf("third reply = %q/%d, want %d runes + ellipsis / %d chars",
			rows[2].ReplySummary, rows[2].ReplyChars, InputIndexReplySummaryLimit, InputIndexReplySummaryLimit+5)
	}
}
