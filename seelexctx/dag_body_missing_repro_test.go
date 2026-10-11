package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 现场复现（第二跳：验收）────────────────────────────────────────────
//
// 第一跳（application/core 的 TestCompactionMaterialMustCarryConversationBody）
// 钉住的是「素材里没有对话正文」。本用例钉住的是**为什么这件事没人发现**：
// 素材里没有正文时，压缩链上没有任何一道闸问过「模型到底有没有被给到要压的内容」，
// 于是模型回一句元回答（「本次请求未附带任何待压缩的对话正文」）就被当成一次
// **成功的模型读后感**落进帧：summary_source=replay、Chapter 2 = 那句元回答。
//
// 修复前（红，两条断言都红）：
//
//	模型被叫到（wire = system→system→user），source=replay，Chapter 2 = 元回答。
//
// 修复后（本用例的口径）：素材里没有一条可压正文时，**这次请求根本不发**，
// 压缩落回本地确定性路径，元回答不会成为 Chapter 2，且成因可从帧读出来——
// 降级证据 `no-replay-material` 指名素材形状；覆盖事实 `replay-coverage:` 指名
// 「素材与要压的区间一条都对不上」。
func TestReplayWithoutConversationBodyMustNotClaimModelReadback(t *testing.T) {
	const metaAnswer = "### 目标 (Goal)\n(本次请求未附带任何待压缩的对话正文，无法生成摘要)"
	chat := &recordingQuickChat{reply: metaAnswer}
	summarizer, err := NewQuickChatPrefixReplaySummarizer(chat)
	if err != nil {
		t.Fatalf("NewQuickChatPrefixReplaySummarizer: %v", err)
	}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		SystemPrompt:      func() string { return "product system prompt" },
		Chapter2MaxTokens: 1024,
		SessionIDProvider: func() string { return "sess-body-missing" },
	})
	input := dagInput(roundHistory(10))
	// 素材 = 上一次真实请求的字节，而那一次会话的引擎历史只剩一条 system 行
	// （现场：messages_before=1）。本次要压的正文在 overflow 里，却不在素材里。
	input.History = []types.Message{textMessage("system", "product system prompt")}

	frame, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// ① 这次重放请求不该发出去：素材里没有可压正文时，模型只会回一句元回答。
	if requests := chat.snapshot(); len(requests) != 0 {
		wire := requests[0].Messages
		t.Fatalf("素材里没有可压的正文，却仍然发了重放请求（%d 条：%s）——"+
			"它只会换来一句元回答，而那句元回答会被当成一次成功的模型读后感",
			len(wire), wireRoles(wire))
	}

	// ② 元回答不得成为本次压缩的 Chapter 2 正文。
	if body := FrameChapter2(frame); strings.Contains(body, "未附带任何待压缩的对话正文") {
		t.Fatalf("模型的元回答成了本次压缩的 Chapter 2 正文：\n%s", body)
	}

	// ③ 不得以 summary_source=replay 落帧：这次没有模型读后感。
	if frame.SummarySource == CompactSummarySourceReplay {
		t.Fatalf("素材里没有可压正文，却以 summary_source=replay 落帧：Chapter 2 = %q", FrameChapter2(frame))
	}

	// ④ 成因要能从帧读出来：素材形状（全是控制行）＋ 素材与要压区间一条都对不上。
	if !hasEvidenceRef(frame, LocalCompactEvidenceRefPrefix+"no-replay-material") {
		t.Fatalf("缺少 no-replay-material 降级证据：%+v", frame.Evidence)
	}
	for _, evidence := range frame.Evidence {
		if evidence.Ref == LocalCompactEvidenceRefPrefix+"no-replay-material" &&
			!strings.Contains(evidence.Summary, "system×1") {
			t.Fatalf("降级原因必须写出素材形状（1 条 system 行）：%q", evidence.Summary)
		}
	}
	uncovered := false
	for _, evidence := range frame.Evidence {
		if strings.HasPrefix(evidence.Ref, CompactReplayCoverageEvidenceRefPrefix) {
			uncovered = true
			if !strings.Contains(evidence.Summary, "0/20") {
				t.Fatalf("覆盖事实必须写出真实数字（要压 20 条载荷行，素材里 0 条）：%q", evidence.Summary)
			}
		}
	}
	if !uncovered {
		t.Fatalf("素材盖不住要压的区间却没有这条事实：%+v", frame.Evidence)
	}
}

// ── 对照组（断言当前行为，不是红灯）────────────────────────────────────
//
// 同一台 DAG + 同一个假 chat，只把素材换成**含对话正文**的那一份：模型给出真的
// 厚摘要，source=replay、Chapter 2 就是那份摘要。
//
// 与上面那条成对：说明"不发请求"不是这台 DAG 的默认行为，而是素材形状判据的结果。
func TestReplayWithConversationBodyStillCompacts(t *testing.T) {
	const thick = "### 目标 (Goal)\n真·厚摘要"
	chat := &recordingQuickChat{reply: thick}
	summarizer, err := NewQuickChatPrefixReplaySummarizer(chat)
	if err != nil {
		t.Fatalf("NewQuickChatPrefixReplaySummarizer: %v", err)
	}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		SystemPrompt:      func() string { return "product system prompt" },
		Chapter2MaxTokens: 1024,
		SessionIDProvider: func() string { return "sess-body-present" },
	})
	input := dagInput(roundHistory(10))
	// 唯一的差别：素材就是本次要压的那段对话（上一次真实请求的字节）。
	input.History = roundHistory(10)

	frame, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if frame.SummarySource != CompactSummarySourceReplay {
		t.Fatalf("素材含对话正文时重放应当成立：summary_source = %q", frame.SummarySource)
	}
	if body := FrameChapter2(frame); !strings.Contains(body, "真·厚摘要") {
		t.Fatalf("Chapter 2 应当是模型给的厚摘要：%q", body)
	}
}

// hasEvidenceRef 报告帧证据里有没有这条 ref（降级出口的唯一解释来源）。
func hasEvidenceRef(frame sessionstore.CompactFrame, ref string) bool {
	for _, evidence := range frame.Evidence {
		if evidence.Ref == ref {
			return true
		}
	}
	return false
}

// wireRoles 渲染一条重放请求的角色构成（红灯里要能一眼看出「素材这一跳没给正文」）。
func wireRoles(messages []types.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, message.Role)
	}
	return strings.Join(parts, "→")
}
