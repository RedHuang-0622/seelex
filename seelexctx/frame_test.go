package seelexctx

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestLocalChapter2SkeletonKeepsSections 详设 §3.2：空章节写 (none)，
// 不丢章节骨架。
func TestLocalChapter2SkeletonKeepsSections(t *testing.T) {
	body := LocalChapter2(LocalCompactOptions{})
	for _, title := range []string{
		Chapter2SectionGoal, Chapter2SectionKeyConcepts,
		Chapter2SectionFilesAndCode, Chapter2SectionErrorsFixes,
		Chapter2SectionPending, Chapter2SectionCurrentWork,
		Chapter2SectionNextStep, Chapter2SectionConstraints,
	} {
		if !strings.Contains(body, "### "+title) {
			t.Fatalf("chapter2 must keep section %q:\n%s", title, body)
		}
	}
	if strings.Count(body, compactEmptySection) != 8 {
		t.Fatalf("empty skeleton must write (none) in all 8 sections:\n%s", body)
	}
}

// TestLocalChapter2OverflowCompact 控制器本地压缩：目标/概念来自任务与计划
// 栈，工具名进文件小节，轮次行进当前工作小节。
func TestLocalChapter2OverflowCompact(t *testing.T) {
	overflow := []historyUnit{
		{messages: []types.Message{
			textMessage("user", "请迁移模块"),
			{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}}}},
			{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: stringPtr("内容")},
		}},
	}
	record := sessionstore.SessionContextRecord{
		TaskStack: []sessionstore.TaskFrame{{TaskID: "task-1", Objective: "迁移上下文控制", Status: "active"}},
		PlanStack: []sessionstore.PlanFrame{{PlanID: "plan-1", Title: "重构计划", Status: "active"}},
	}
	body := LocalChapter2(LocalCompactOptions{
		Overflow: overflow, UnitCount: 1, Kind: LocalCompactOverflow, Record: record,
	})
	for _, want := range []string{"迁移上下文控制", "重构计划 (active)", "read_file", "溢出轮次: 1 个完整协议单元"} {
		if !strings.Contains(body, want) {
			t.Fatalf("chapter2 must contain %q:\n%s", want, body)
		}
	}
}

// TestLocalChapter2GapCompact 真空区压缩：文案用真空区轮次并保留上一栈顶
// Chapter 2 正文（本地路径的栈顶自足近似）。
func TestLocalChapter2GapCompact(t *testing.T) {
	prev := sessionstore.CompactFrame{Summary: "## 压缩内容 (Compacted Context)\n### 当前工作 (Current Work)\n先前内容行"}
	overflow := []historyUnit{{messages: []types.Message{textMessage("user", "round D")}}}
	body := LocalChapter2(LocalCompactOptions{
		Overflow: overflow, UnitCount: 4, Kind: LocalCompactGap, PrevTop: &prev,
	})
	for _, want := range []string{
		"真空区轮次: 4 个完整协议单元",
		"先前压缩摘要: ### 当前工作 (Current Work)\n先前内容行",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("chapter2 must contain %q:\n%s", want, body)
		}
	}
}

// TestFrameSummaryRoundTripAndViews 拼装 → 截取：Summary 固定两章节；
// FrameChapter2 只取 Chapter 2 正文；FrameChapter1 取锚点正文。
func TestFrameSummaryRoundTripAndViews(t *testing.T) {
	prev := sessionstore.CompactFrame{
		SegmentID: "compact-sess-1", RequestFrom: "chat-1", RequestTo: "chat-7",
		Summary: "## 压缩内容 (Compacted Context)\n### 当前工作 (Current Work)\n完成了模块 X 的迁移与验收",
	}
	anchor := RenderAnchorChapter(&prev)
	if !strings.Contains(anchor, "segment_id: compact-sess-1") ||
		!strings.Contains(anchor, "request: chat-1 .. chat-7") ||
		!strings.Contains(anchor, "一句话: 完成了模块 X 的迁移与验收") {
		t.Fatalf("anchor chapter = %q", anchor)
	}
	frame := sessionstore.CompactFrame{Summary: RenderFrameSummary(anchor, "### 目标 (Goal)\n继续迁移")}
	if got := FrameChapter1(frame); !strings.Contains(got, "segment_id: compact-sess-1") {
		t.Fatalf("chapter1 view = %q", got)
	}
	if got := FrameChapter2(frame); got != "### 目标 (Goal)\n继续迁移" {
		t.Fatalf("chapter2 view = %q", got)
	}
}

// TestFrameChapter2LegacyFallback 旧记录 Summary 无章节标记 → Chapter 2
// 视图退化为整段摘要（向后兼容）。
func TestFrameChapter2LegacyFallback(t *testing.T) {
	legacy := sessionstore.CompactFrame{Summary: "任务目标: 旧式摘要行"}
	if got := FrameChapter2(legacy); got != "任务目标: 旧式摘要行" {
		t.Fatalf("legacy chapter2 view = %q", got)
	}
	if got := FrameChapter1(legacy); got != "" {
		t.Fatalf("legacy chapter1 view must be empty, got %q", got)
	}
}

// TestOneLineSummaryAndAnchorSource 一句话摘要与锚点质量标记：
// 能提取 → ok；只含 (none)/空 → degraded；无前驱 → 空。
func TestOneLineSummaryAndAnchorSource(t *testing.T) {
	full := sessionstore.CompactFrame{Summary: "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n迁移完成"}
	if got := OneLineSummary(full); got != "迁移完成" {
		t.Fatalf("one line = %q", got)
	}
	if got := FrameAnchorSource(&full); got != CompactAnchorSourceOK {
		t.Fatalf("anchor source = %q, want ok", got)
	}
	empty := sessionstore.CompactFrame{Summary: "## 压缩内容 (Compacted Context)\n### 待办 (Pending)\n(none)"}
	if got := OneLineSummary(empty); got != "" {
		t.Fatalf("one line of empty body = %q", got)
	}
	if got := FrameAnchorSource(&empty); got != CompactAnchorSourceDegraded {
		t.Fatalf("anchor source = %q, want degraded", got)
	}
	if got := FrameAnchorSource(nil); got != "" {
		t.Fatalf("anchor source of first frame = %q, want empty", got)
	}
	// 长行截断（rune 上限）。
	long := sessionstore.CompactFrame{Summary: "## 压缩内容 (Compacted Context)\n" + strings.Repeat("长", 90)}
	if got := OneLineSummary(long); len([]rune(got)) > maxOneLineRunes {
		t.Fatalf("one line must be truncated, got %d runes", len([]rune(got)))
	}
}

// TestOneLineSummarySkipsStructuredData 锚点一句话不得取到结构化数据行。
//
// 有牙：删掉 OneLineSummary 里那条 IndexAny(`{["` + 反引号) 守卫，本用例即红。
// 这条污染是**静默**的——每帧看起来都"有锚点"（非空 → anchor_source=ok），
// 不会触发 degraded 标记，因此不会有任何报警，而下一帧的 Chapter 1 里已经躺着
// 一段花括号。回读帧正文的 JSON 元数据块正是以 `{` 开头（见
// application/core/context_runtime/compaction_frame.go 的规范形状）。
func TestOneLineSummarySkipsStructuredData(t *testing.T) {
	polluted := sessionstore.CompactFrame{Summary: "## " + CompactChapter2Title + "\n" +
		"{\n  \"schema\": \"seelex.context-compaction-frame/v2\",\n  \"version\": 7\n}\n" +
		"### 目标 (Goal)\n接通压缩帧索引面"}
	if got := OneLineSummary(polluted); got != "接通压缩帧索引面" {
		t.Fatalf("锚点取到了结构化数据行 = %q", got)
	}
	if got := FrameAnchorSource(&polluted); got != CompactAnchorSourceOK {
		t.Fatalf("anchor source = %q, want ok", got)
	}
	// 数组、引号、反引号开头同样不得入选（fenced 代码块与 JSON 数组都会出现）。
	for _, line := range []string{`["a","b"]`, `"quoted": true`, "```json"} {
		frame := sessionstore.CompactFrame{Summary: "## " + CompactChapter2Title + "\n" + line}
		if got := OneLineSummary(frame); got != "" {
			t.Fatalf("行首 %q 不得成为锚点，got %q", line, got)
		}
		if got := FrameAnchorSource(&frame); got != CompactAnchorSourceDegraded {
			t.Fatalf("行首 %q 应标记 degraded，got %q", line, got)
		}
	}
}

// TestChatQueueRequestLabels 1 基 request 覆盖标签；倒置返回空。
func TestChatQueueRequestLabels(t *testing.T) {
	from, to := ChatQueueRequestLabels(0, 6)
	if from != "chat-1" || to != "chat-7" {
		t.Fatalf("labels = %q..%q", from, to)
	}
	from, to = ChatQueueRequestLabels(9, 4)
	if from != "" || to != "" {
		t.Fatalf("inverted labels = %q..%q", from, to)
	}
}
