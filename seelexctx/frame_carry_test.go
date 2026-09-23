package seelexctx

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// carryTestFrame 构造一个带 Chapter 2 正文与完整链锚字段的帧（前一栈顶）。
func carryTestFrame(body string) sessionstore.CompactFrame {
	return sessionstore.CompactFrame{
		SegmentID:   "compact-sess-7-1700000000000",
		From:        3,
		To:          9,
		RequestFrom: "chat-4",
		RequestTo:   "chat-10",
		Summary:     RenderFrameSummary(RenderAnchorChapter(nil), body),
	}
}

// TestCarryPreviousChapter2KeepsSmallBody：并入量在限额内 → Chapter 2 正文原样并入
// （栈顶自足），决策事实标记为未降级。
func TestCarryPreviousChapter2KeepsSmallBody(t *testing.T) {
	prev := carryTestFrame("### 目标 (Goal)\n继续迁移前缀链路")
	body, facts := CarryPreviousChapter2(&prev, 1024)
	if body != strings.TrimSpace(FrameChapter2(prev)) {
		t.Fatalf("并入正文被改写：\n%q", body)
	}
	if facts.Anchor {
		t.Fatalf("未超限却被标记为锚点降级：%+v", facts)
	}
	if facts.LimitTokens != 1024 || facts.CarriedTokens != tokens.Count(body) {
		t.Fatalf("决策事实不完整：%+v（正文 %d token）", facts, tokens.Count(body))
	}
}

// TestCarryPreviousChapter2DegradesToAnchor：并入量超限 → 退化为锚点（segment_id +
// request 首尾 + 一句话 + 读回提示），正文不再随帧数膨胀。
func TestCarryPreviousChapter2DegradesToAnchor(t *testing.T) {
	huge := "### 当前工作 (Current Work)\n" + strings.Repeat("carry material line: keep the frame body bounded. ", 200)
	prev := carryTestFrame(huge)
	const limit = 200
	body, facts := CarryPreviousChapter2(&prev, limit)
	if !facts.Anchor {
		t.Fatalf("超限未标记锚点降级：%+v", facts)
	}
	if facts.CarriedTokens <= limit {
		t.Fatalf("决策事实里的并入量 %d 应大于上限 %d", facts.CarriedTokens, limit)
	}
	for _, want := range []string{
		"超出并入上限", "segment_id: compact-sess-7-1700000000000",
		"request: chat-4 .. chat-10", "read_compressed_turn",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("锚点退化正文缺少 %q：\n%s", want, body)
		}
	}
	if strings.Contains(body, strings.Repeat("carry material line", 50)) {
		t.Fatalf("超限后仍并入了上一帧大段正文（上限没有生效）")
	}
	if tokens.Count(body) >= tokens.Count(huge) {
		t.Fatalf("退化后正文 %d token 不应大于原正文 %d token", tokens.Count(body), tokens.Count(huge))
	}
}

// TestCarryPreviousChapter2DefaultsAndEmpty：limit <= 0 → 用兜底定值；无上一帧 /
// 空正文 → 空串且不带降级标记。
func TestCarryPreviousChapter2DefaultsAndEmpty(t *testing.T) {
	prev := carryTestFrame("### 目标 (Goal)\n短正文")
	if _, facts := CarryPreviousChapter2(&prev, 0); facts.LimitTokens != DefaultFrameCarryTokens {
		t.Fatalf("limit<=0 应回退兜底定值 %d，得到 %+v", DefaultFrameCarryTokens, facts)
	}
	if body, facts := CarryPreviousChapter2(nil, 1024); body != "" || facts.LimitTokens != 0 {
		t.Fatalf("无上一帧时应为空串且无决策事实，得到 %q %+v", body, facts)
	}
	// 旧记录（Summary 为空）→ FrameChapter2 退化为空串，因此不产生并入内容，
	// 也不该被误标为锚点降级。
	empty := sessionstore.CompactFrame{SegmentID: "compact-legacy"}
	if body, facts := CarryPreviousChapter2(&empty, 1024); body != "" || facts.Anchor {
		t.Fatalf("空摘要不应产生锚点降级，得到 %q %+v", body, facts)
	}
}

// TestCarryEvidenceAndAnchorSource：并入决策的**打点落点**——帧证据里留一条可审计
// 记录（无上一帧不写），且降级时链锚标记写 degraded（读帧的人据此知道正文不是原文）。
func TestCarryEvidenceAndAnchorSource(t *testing.T) {
	if evidence := CarryEvidence(CarryDiagnostics{}); evidence != nil {
		t.Fatalf("无上一帧不应写证据，得到 %+v", evidence)
	}
	kept := CarryEvidence(CarryDiagnostics{LimitTokens: 1024, CarriedTokens: 300})
	if len(kept) != 1 || !strings.Contains(kept[0].Ref, "frame-carry:kept:300/1024") {
		t.Fatalf("未降级的证据形状不对：%+v", kept)
	}
	anchor := CarryEvidence(CarryDiagnostics{LimitTokens: 1024, CarriedTokens: 5000, Anchor: true})
	if len(anchor) != 1 || !strings.HasPrefix(anchor[0].Ref, "frame-carry:anchor:5000/1024") {
		t.Fatalf("降级的证据形状不对：%+v", anchor)
	}
	if !strings.Contains(anchor[0].Summary, "read_compressed_turn") {
		t.Fatalf("降级证据应指明回读手段：%q", anchor[0].Summary)
	}
	if got := AnchorSourceWithCarry(CompactAnchorSourceOK, CarryDiagnostics{LimitTokens: 1024, CarriedTokens: 5000, Anchor: true}); got != CompactAnchorSourceDegraded {
		t.Fatalf("降级时锚点标记应为 %q，得到 %q", CompactAnchorSourceDegraded, got)
	}
	if got := AnchorSourceWithCarry(CompactAnchorSourceOK, CarryDiagnostics{LimitTokens: 1024, CarriedTokens: 300}); got != CompactAnchorSourceOK {
		t.Fatalf("未降级时应保持原标记，得到 %q", got)
	}
}

// TestLocalChapter2WithCarryReportsFacts：本地折叠的 Chapter 2 与决策事实一次算出
// （同一份并入量不重复计算），并入受限额约束。
func TestLocalChapter2WithCarryReportsFacts(t *testing.T) {
	prev := carryTestFrame("### 目标 (Goal)\n" + strings.Repeat("long previous body ", 300))
	chapter2, facts := LocalChapter2WithCarry(LocalFoldOptions{
		Overflow: nil, UnitCount: 2, Kind: CompactFoldOverflow,
		PrevTop: &prev, CarryLimitTokens: 128,
	})
	if !facts.Anchor {
		t.Fatalf("超限未降级：%+v", facts)
	}
	if !strings.Contains(chapter2, "## 当前工作 (Current Work)") {
		t.Fatalf("Chapter 2 丢了小节骨架：\n%s", chapter2)
	}
	if !strings.Contains(chapter2, "先前压缩摘要: ") {
		t.Fatalf("Chapter 2 应保留并入段（哪怕是退化的锚点）：\n%s", chapter2)
	}
	if strings.Count(chapter2, "先前压缩摘要: ") != 1 {
		t.Fatalf("并入段出现次数不为 1：\n%s", chapter2)
	}
	// LocalChapter2 是同一实现的取值视图：两处必须逐字节一致（不做第二份拼装）。
	if got := LocalChapter2(LocalFoldOptions{
		Overflow: nil, UnitCount: 2, Kind: CompactFoldOverflow,
		PrevTop: &prev, CarryLimitTokens: 128,
	}); got != chapter2 {
		t.Fatalf("LocalChapter2 与 LocalChapter2WithCarry 结果不一致")
	}
}
