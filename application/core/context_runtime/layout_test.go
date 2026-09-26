package context_runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// layoutTestCounter 是四区切分的确定性计数：system 段 3、每条历史 10、当轮输入
// 按字节 1。判据侧与报表侧用的是同一个注入点，因此测试断言的是"分区与计数口径"，
// 不是某一次真实估算的数值。
func layoutTestCounter(systemPrompt string, history []contract.EngineMessage, currentInput string, _ []model.Tool) int {
	count := 0
	if strings.TrimSpace(systemPrompt) != "" {
		count += 3
	}
	count += 10 * len(history)
	count += len(currentInput)
	return count
}

// TestContextZonesClassifiesFourZones：四区显式化的分区判据全部是消息自身的
// 事实（role + 前缀标记），不做位置推算：稳定 system → ①；压缩帧标记 → ②；
// 定稿轮次 → ③；plan 尾部 → ④；当轮输入单列。
func TestContextZonesClassifiesFourZones(t *testing.T) {
	assembled := []contract.EngineMessage{
		{Role: "system", Content: "identity + skill catalog", ContentSet: true},
		{Role: "system", Content: "project block", ContentSet: true},
		{Role: "user", Content: seelexctx.CompactContextMarker + " segment=c-1 from=0 to=3", ContentSet: true},
		{Role: "user", Content: "settled request", ContentSet: true},
		{Role: "assistant", Content: "settled answer", ContentSet: true},
		{Role: "system", Content: planContextPrefix + "\n{}", ContentSet: true},
	}
	zones := ContextZones("engine system prompt", assembled, "current input", layoutTestCounter)
	if len(zones) != 5 {
		t.Fatalf("分区数 = %d，want 5（① ② ③ ④ + 当轮输入）", len(zones))
	}
	want := []struct {
		kind     string
		messages int
		tokens   int
	}{
		{ZoneStable, 3, 10 * 3}, // engine system prompt + 2 条 system（计数注入在 messages 上）
		{ZoneFolded, 1, 10},
		{ZoneProtected, 2, 20},
		{ZoneTail, 1, 10},
		{ZoneInput, 1, len("current input")},
	}
	for index, expected := range want {
		zone := zones[index]
		if zone.Kind != expected.kind || zone.Messages != expected.messages || zone.Tokens != expected.tokens {
			t.Fatalf("分区 %d = %+v，want kind=%s messages=%d tokens=%d",
				index, zone, expected.kind, expected.messages, expected.tokens)
		}
		if strings.TrimSpace(zone.Source) == "" {
			t.Fatalf("分区 %s 缺少来源说明：%+v", zone.Kind, zone)
		}
	}
	if other := ContextZones("", assembled, "", nil); other != nil {
		t.Fatalf("没有计数器时不产出分区，得到 %+v", other)
	}
	// 未折叠时 ② 为 0（没有任何内容被折出），③ 即全部累积的已定稿轮次。
	accumulating := ContextZones("", assembled[3:5], "", layoutTestCounter)
	if accumulating[1].Tokens != 0 || accumulating[2].Tokens != 20 {
		t.Fatalf("未折叠轮次的四区口径不对：%+v", accumulating)
	}
}

// TestRetainWindowDecisionRecordsFacts：保留窗口决策把"拿什么数字比的"全部记下
// （token1/token2/floor/最终值），且 floor_applied 只在**下限真的抬高结果**时为真。
func TestRetainWindowDecisionRecordsFacts(t *testing.T) {
	budget := task_context.ContextBudget{Window: 200_000, Budget: 174_488}
	config := seelexctx.WindowConfig{Ratio: 0.7}
	base := retainWindowDecision(config, 100_000, budget, 0)
	if base.Retained != 70_000 || base.RatioTokens != 70_000 || base.FloorTokens != 0 || base.FloorApplied {
		t.Fatalf("未配下限的决策事实 = %+v，want retained=70000 且 floor 未生效", base)
	}
	// 占比窗口把保留区压到 84000，下限（60% × 预算 = 104692）真的抬高了它。
	raised := retainWindowDecision(config, 120_000, budget, 60)
	if raised.FloorTokens != 104_692 || raised.Retained != 104_692 || !raised.FloorApplied {
		t.Fatalf("下限抬高结果的决策事实 = %+v，want floor=retained=104692 且已生效", raised)
	}
	// 下限没生效时 floor_applied 必须为假（报表不得把"配了但没用上"报成生效）。
	noEffect := retainWindowDecision(seelexctx.WindowConfig{Ratio: 0.9}, 120_000, budget, 10)
	if noEffect.FloorTokens != 17_448 || noEffect.Retained != 108_000 || noEffect.FloorApplied {
		t.Fatalf("下限未抬高结果时不该标记生效：%+v", noEffect)
	}
	clamped := retainWindowDecision(config, 100_000, budget, 120)
	if clamped.Retained != 100_000 {
		t.Fatalf("下限超过全量上下文时应夹到全量，得到 %d", clamped.Retained)
	}
	capped := retainWindowDecision(seelexctx.WindowConfig{Ratio: 0.7, RetainTokens: 60_000}, 100_000, budget, 60)
	if capped.CapTokens != 60_000 || capped.Retained != 60_000 {
		t.Fatalf("保留上限必须拦住下限：%+v", capped)
	}
	// 压缩目标（context_target_percent × 预算）是保留区的硬上限：占比窗口
	// 允许 126000，也必须收口到目标 100000，给下一轮留出 soft − target 的余量。
	targetCapped := retainWindowDecision(config, 180_000,
		task_context.ContextBudget{Window: 200_000, Budget: 174_488, TargetAfterCompaction: 100_000}, 0)
	if targetCapped.RatioTokens <= 100_000 || targetCapped.TargetTokens != 100_000 ||
		targetCapped.Retained != 100_000 || !targetCapped.TargetApplied {
		t.Fatalf("压缩目标未收口保留区：%+v", targetCapped)
	}
}

// TestContextLayoutTerseAndZonesRender：门禁 Detail 与帧正文读同一份 layout——
// 一行事实（判据用）与四区区块（报表用）都由同一个结构渲染。
func TestContextLayoutTerseAndZonesRender(t *testing.T) {
	layout := ContextLayout{
		Zones: []ContextZone{
			{Kind: ZoneStable, Tokens: 1200, Messages: 2, Source: "system"},
			{Kind: ZoneFolded, Tokens: 90_000, Messages: 1, Source: "folded units"},
			{Kind: ZoneProtected, Tokens: 20_000, Messages: 8, Source: "retained window"},
			{Kind: ZoneTail, Tokens: 200, Messages: 1, Source: "plan tail"},
			{Kind: ZoneInput, Tokens: 30, Messages: 1, Source: "current input"},
		},
		Retain:          RetainDecision{AllContextTokens: 130_000, BudgetTokens: 174_488, CapTokens: 120_000, RatioTokens: 91_000, FloorTokens: 20_938, Retained: 91_000, FloorApplied: false},
		ComparedTokens:  130_066,
		EstimatedTokens: 91_120,
		SoftThreshold:   130_866,
		HardThreshold:   157_039,
		Compacting:      true,
	}
	terse := layout.ZonesTerse()
	for _, want := range []string{"stable_prefix=1200", "folded=90000", "protected_window=20000", "tail=200", "current_input=30"} {
		if !strings.Contains(terse, want) {
			t.Fatalf("分区一行事实缺少 %q：%s", want, terse)
		}
	}
	retain := layout.RetainTerse()
	for _, want := range []string{"all=130000", "cap=120000", "ratio=91000", "floor=20938", "retained=91000", "floor_applied=false"} {
		if !strings.Contains(retain, want) {
			t.Fatalf("保留窗口一行事实缺少 %q：%s", want, retain)
		}
	}
	rendered := layout.RenderZones()
	for _, want := range []string{"- stable_prefix:", "- folded:", "- protected_window:", "- tail:", "- current_input:", "- 判据:", "- 保留窗口:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("帧正文四区区块缺少 %q：\n%s", want, rendered)
		}
	}
	if layout.zone(ZoneFolded).Tokens != 90_000 || layout.zone("unknown").Tokens != 0 {
		t.Fatalf("zone 查询口径不对：%+v", layout.zone("unknown"))
	}
}

// TestCompactionFrameBodyCarriesZoneLayout：帧正文必须带上四区区块——否则记录里
// 只剩一个总量，读者无法把"判据说超了"与"哪个区占了多少"对上。
func TestCompactionFrameBodyCarriesZoneLayout(t *testing.T) {
	layout := ContextLayout{
		Zones:  []ContextZone{{Kind: ZoneStable, Tokens: 100, Messages: 1, Source: "system"}},
		Retain: RetainDecision{AllContextTokens: 200_000, BudgetTokens: 174_488, Retained: 91_000},
	}
	body := compactionFrameBody(compactionFrameInput{
		Version: 3, Reason: "context_budget", Origin: "explicit_after_turn",
		At:       time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC),
		Injected: false, Layout: layout,
	})
	for _, want := range []string{"## Context zones (四区)", "- stable_prefix:", "- 保留窗口:", "retained=91000"} {
		if !strings.Contains(body, want) {
			t.Fatalf("帧正文缺少 %q：\n%s", want, body)
		}
	}
	// 未提供 layout（旧调用方/无装配上下文）时不留空段。
	plain := compactionFrameBody(compactionFrameInput{Version: 1, Reason: "context_budget"})
	if strings.Contains(plain, "## Context zones") {
		t.Fatalf("没有 layout 时不得写空区块：\n%s", plain)
	}
}
