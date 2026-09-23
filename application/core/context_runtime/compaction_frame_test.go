package context_runtime

import (
	"strings"
	"testing"
	"time"
)

// TestCompactionFrameBodyReportsFoldedRangeAndInjection：帧正文（前端按 ref
// 回读的那一份）必须把三类事实分开写：折掉了哪一段、模型现在拿到的替代物、
// 留下的有界证据。特别是 injected：普通显式压缩走保留窗口路径，并没有把证据
// 摘要注入 provider 历史，写成"模型已看到"就是假话。
func TestCompactionFrameBodyReportsFoldedRangeAndInjection(t *testing.T) {
	body := compactionFrameBody(compactionFrameInput{
		Version:         7,
		Reason:          "context_budget",
		Origin:          "explicit_after_turn",
		At:              time.Date(2026, 9, 23, 16, 40, 2, 0, time.UTC),
		RangeLabel:      "消息 message-1..message-103 / 事件 1..6",
		ComparedTokens:  129_409,
		AssembledTokens: 20_480,
		SoftThreshold:   118_962,
		HardThreshold:   142_754,
		Evidence:        "objective: 修复 compact\ncompleted=\"改了记录门槛\"\n",
		PlanMessage:     `{"plan_id":"p-1"}`,
		Injected:        false,
	})
	for _, want := range []string{
		compactionFrameMarker,
		"# Context checkpoint frame v7",
		"reason: context_budget · origin: explicit_after_turn",
		"folded: 消息 message-1..message-103 / 事件 1..6",
		"tokens: compared 129409 → assembled 20480 (soft 118962 / hard 142754)",
		"injected: no",
		"objective: 修复 compact",
		"## Plan tail (kept in provider history)",
		`{"plan_id":"p-1"}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("帧正文缺少 %q：\n%s", want, body)
		}
	}
	if strings.Contains(body, "injected: yes") {
		t.Fatalf("保留窗口路径不得写成已注入 provider 历史：\n%s", body)
	}
}

// TestCompactionFrameBodyAdmitsMissingEvidence：没有可回读证据/没有区间边界时
// 如实说明，不留空段也不编造数字（自主压缩帧正文已注入 provider 时写 injected: yes）。
func TestCompactionFrameBodyAdmitsMissingEvidence(t *testing.T) {
	body := compactionFrameBody(compactionFrameInput{
		Version:         2,
		Reason:          "context_budget_autonomous",
		Origin:          "auto",
		At:              time.Date(2026, 9, 23, 16, 40, 2, 0, time.UTC),
		ComparedTokens:  10,
		AssembledTokens: 5,
		SoftThreshold:   8,
		HardThreshold:   9,
		Injected:        true,
	})
	for _, want := range []string{
		"folded: 本次没有可记的区间边界",
		"injected: yes",
		"没有可回读的任务证据摘要",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("帧正文缺少 %q：\n%s", want, body)
		}
	}
	if strings.Contains(body, "## Plan tail") {
		t.Fatalf("没有 plan 尾部时不得写出该段：\n%s", body)
	}
}
