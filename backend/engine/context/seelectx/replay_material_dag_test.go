package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// DAG 集成：破损的重放素材在进 chapter2 之前就被规整，模型收到的是合法字节序；
// 素材被动过的事实进帧证据（不静默改字节）。现场形状见 replay_material_test.go
// 文件头注释。

// TestCompactionDAGDropsUnsettledTailBeforeReplay：素材尾巴是未落定的工具调用单元
// （工具正在跑 / 上一轮被中断）→ 丢该单元后再重放，帧留下规整证据。
func TestCompactionDAGDropsUnsettledTailBeforeReplay(t *testing.T) {
	summarizer := &recordingSummarizer{}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		ReplayInputTokens: 1_000_000,
		SessionIDProvider: func() string { return "sess-material" },
	})
	history := []types.Message{
		textMessage("user", "任务1：核对装配路径"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "file body"),
	}
	frame, err := dag.Execute(context.Background(), CompactionInput{
		Messages: roundHistory(3), History: history, UnitCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summarizer.requests) != 1 {
		t.Fatalf("重放次数 = %d, want 1（素材规整后仍应重放）", len(summarizer.requests))
	}
	sent := summarizer.requests[0].History
	if err := ValidateReplayProtocol(sent); err != nil {
		t.Fatalf("实发素材必须合法：%v", err)
	}
	if len(sent) != 1 || sent[0].Role != "user" {
		t.Fatalf("未落定尾单元不得进请求：%+v", sent)
	}
	if frame.SummarySource != CompactSummarySourceReplay {
		t.Fatalf("summary_source = %q, want replay", frame.SummarySource)
	}
	found := false
	for _, evidence := range frame.Evidence {
		if strings.HasPrefix(evidence.Ref, CompactReplayMaterialEvidenceRefPrefix) {
			found = true
			if !strings.Contains(evidence.Summary, "dropped unsettled tail unit") ||
				!strings.Contains(evidence.Summary, "t2") {
				t.Fatalf("证据必须说清动了什么：%q", evidence.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("素材被规整过却没有证据：%+v", frame.Evidence)
	}
}

// TestCompactionDAGFillsDeadChainBeforeReplay：中段死链（声明之后还有别的消息）
// → 补回执占位后再重放，素材里出现一条 recovery 占位（与装配层对真实请求补的
// 占位同款），其余字节原样。
func TestCompactionDAGFillsDeadChainBeforeReplay(t *testing.T) {
	summarizer := &recordingSummarizer{}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		ReplayInputTokens: 1_000_000,
	})
	history := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "file body"),
		textMessage("assistant", "已完成第一步"),
		textMessage("user", "任务2"),
	}
	frame, err := dag.Execute(context.Background(), CompactionInput{
		Messages: roundHistory(2), History: history, UnitCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	sent := summarizer.requests[0].History
	if err := ValidateReplayProtocol(sent); err != nil {
		t.Fatalf("实发素材必须合法：%v", err)
	}
	if len(sent) != len(history)+1 {
		t.Fatalf("素材长度 = %d, want %d（补一条占位）", len(sent), len(history)+1)
	}
	placeholder := sent[3]
	if placeholder.Role != "tool" || placeholder.ToolCallID != "t2" ||
		placeholder.Content == nil || !strings.HasPrefix(*placeholder.Content, interruptedToolResultPrefix) {
		t.Fatalf("占位必须落在声明结果块内：%+v", placeholder)
	}
	if frame.SummarySource != CompactSummarySourceReplay {
		t.Fatalf("summary_source = %q, want replay", frame.SummarySource)
	}
}

// TestCompactionDAGLegalMaterialStaysByteForByteAndSilent：合法素材既不改字节
// 也不留证据（前缀缓存口径 + "没这件事就不留痕"）。
func TestCompactionDAGLegalMaterialStaysByteForByteAndSilent(t *testing.T) {
	summarizer := &recordingSummarizer{}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		ReplayInputTokens: 1_000_000,
	})
	history := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file")),
		replayToolResult("t1", "read_file", "body"),
		textMessage("assistant", "第一步完成"),
	}
	frame, err := dag.Execute(context.Background(), CompactionInput{
		Messages: roundHistory(2), History: history, UnitCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	sent := summarizer.requests[0].History
	if !sameReplayMessages(sent, history) {
		t.Fatalf("合法素材必须逐字重放：%+v", sent)
	}
	for _, evidence := range frame.Evidence {
		if strings.HasPrefix(evidence.Ref, CompactReplayMaterialEvidenceRefPrefix) {
			t.Fatalf("合法素材不该留规整证据：%+v", frame.Evidence)
		}
	}
}

// TestCompactionDAGMaterialTrimmedToEmptyFallsBackToLocal：素材整体就是一个未落定
// 的工具调用单元 → 规整后为空，不发一次没有正文的重放，改走本地压缩并把原因写成
// `no-replay-material`（不猜、不静默）。
func TestCompactionDAGMaterialTrimmedToEmptyFallsBackToLocal(t *testing.T) {
	summarizer := &recordingSummarizer{}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		ReplayInputTokens: 1_000_000,
	})
	frame, err := dag.Execute(context.Background(), CompactionInput{
		Messages: roundHistory(2), UnitCount: 2,
		History: []types.Message{replayAssistantCall(replayToolCall("t1", "read_file"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summarizer.requests) != 0 {
		t.Fatalf("素材为空时不得调用模型，得到 %d 次", len(summarizer.requests))
	}
	if frame.SummarySource != CompactSummarySourceLocal {
		t.Fatalf("summary_source = %q, want local", frame.SummarySource)
	}
	found := false
	for _, evidence := range frame.Evidence {
		if evidence.Ref == LocalCompactEvidenceRefPrefix+"no-replay-material" {
			found = true
			if !strings.Contains(evidence.Summary, "t1") {
				t.Fatalf("降级原因必须指名未回执的调用：%q", evidence.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("缺少 no-replay-material 降级证据：%+v", frame.Evidence)
	}
}
