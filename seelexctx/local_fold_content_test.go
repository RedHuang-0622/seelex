package seelexctx

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/types"
)

// 本地确定性折叠（索引）的内容面：被折轮次的**助手答复**必须进帧。
//
// 背景（2026-09-30 重启恢复现场）：本地折叠的 Chapter 2 是模型唯一能看到的被折内容
// （assembler 只渲染栈顶帧 Chapter 2）。此前 renderUnitLine 的 assistant 分支只输出
// 工具调用名，助手正文一个字都不进帧——于是重启（冷加载）后，被折掉的轮次在模型眼里
// 只剩"- 用户: 前 80 字"的索引，"助手回答了什么"完全不见，压缩从"总结上下文"退化成
// "折叠掉上下文"。

// TestLocalChapter2CarriesAssistantAnswerPreview 有牙：删掉 renderUnitLine 里
// assistant 的正文预览分支，本用例即红。
func TestLocalChapter2CarriesAssistantAnswerPreview(t *testing.T) {
	const answer = "根因：A3 写者在提交临界区里重入了读路径，第二条用例因此拿到过期快照。"
	overflow := []historyUnit{{messages: []types.Message{
		textMessage("user", "这里的根因是什么？"),
		textMessage("assistant", answer+strings.Repeat("C", 4000)),
	}}}
	body := LocalChapter2(LocalFoldOptions{Overflow: overflow, UnitCount: 1, Kind: CompactFoldOverflow})
	if !strings.Contains(body, "- 助手: ") {
		t.Fatalf("本地折叠必须留下助手答复行（否则模型看不到被折内容）：\n%s", body)
	}
	if !strings.Contains(body, answer) {
		t.Fatalf("助手答复首段必须进帧（截断不得吃掉开头）：\n%s", body)
	}
	// 判据 R1 的另一半：索引不含正文这件事要与回读入口一起写出来。
	for _, want := range []string{"折叠索引", "read_compressed_turn", "search_history"} {
		if !strings.Contains(body, want) {
			t.Fatalf("本地折叠应写明这是索引并给出回读入口 %q：\n%s", want, body)
		}
	}
}

// TestLocalChapter2AssistantPreviewBoundedByRune 预览按 rune 截断（多字节安全），
// 且带工具调用的助手轮次同时留下正文预览与工具调用名。
func TestLocalChapter2AssistantPreviewBoundedByRune(t *testing.T) {
	long := strings.Repeat("中", 200)
	line := renderUnitLine([]types.Message{
		textMessage("assistant", long),
		{Role: "assistant", Content: stringPtr(long),
			ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}}}},
	})
	if !utf8.ValidString(line) {
		t.Fatalf("按 byte 截断会把多字节字符切坏：%q", line)
	}
	for _, want := range []string{"- 助手: ", "- 工具调用: read_file"} {
		if !strings.Contains(line, want) {
			t.Fatalf("轮次行缺少 %q：\n%s", want, line)
		}
	}
	for _, rendered := range strings.Split(strings.TrimSpace(line), "\n") {
		preview, ok := strings.CutPrefix(rendered, "- 助手: ")
		if !ok {
			continue
		}
		if runes := utf8.RuneCountInString(preview); runes > maxUnitPreviewRunes {
			t.Fatalf("助手预览 %d runes 超过上限 %d：%q", runes, maxUnitPreviewRunes, preview)
		}
	}
	// 空助手正文（只有工具调用）不得留下空行。
	empty := renderUnitLine([]types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "bash"}}}},
	})
	if strings.Contains(empty, "- 助手: ") {
		t.Fatalf("无正文的助手轮次不该留空预览行：\n%s", empty)
	}
}

// TestCompactionDAGNoSummarizerNoteStaysHonest 未注入摘要器且调用方未说明原因时，
// 帧自答不得把原因写成装配层的"开关关闭或 QuickChat 装配失败"——结构性未装配会被
// 读成配置事故（2026-09-30 现场）。调用方给出的 note 则原样进帧。
func TestCompactionDAGNoSummarizerNoteStaysHonest(t *testing.T) {
	noteOf := func(t *testing.T, opts CompactionDAGOptions) string {
		t.Helper()
		frame, err := NewCompactionDAG(opts).Execute(t.Context(), dagInput(roundHistory(3)))
		if err != nil {
			t.Fatal(err)
		}
		for _, evidence := range frame.Evidence {
			if evidence.Ref == CompactFoldLocalEvidenceRefPrefix+"no-summarizer" {
				return evidence.Summary
			}
		}
		t.Fatalf("本地折叠应留 fold-local:no-summarizer 证据：%+v", frame.Evidence)
		return ""
	}
	defaultNote := noteOf(t, CompactionDAGOptions{})
	if strings.Contains(defaultNote, "开关关闭或 QuickChat 装配失败") {
		t.Fatalf("未说明原因时不得冒充配置/装配事故：%q", defaultNote)
	}
	if !strings.Contains(defaultNote, "未说明原因") {
		t.Fatalf("未说明原因时应如实写「未说明」：%q", defaultNote)
	}
	const supplied = "结构性：这条链路不注入摘要器"
	if got := noteOf(t, CompactionDAGOptions{SummarizerNote: supplied}); got != supplied {
		t.Fatalf("调用方给出的 note 应原样进帧：got %q want %q", got, supplied)
	}
}
