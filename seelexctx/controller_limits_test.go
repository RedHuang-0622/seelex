package seelexctx

// 阈值来源互钉：soft/hard/target 与安全预留除数必须来自 limits 配置段，
// 工具结果归档上限必须由调用方注入生效值。
//
// 这组用例钉的是 2026-09-25 修掉的漂移：框架侧 ContextWindowPolicy 把
// 75/90/60 与 window/8 硬编码在代码里，而 limits 段的
// context_soft_percent / context_hard_percent / context_target_percent /
// context_safety_reserve_divisor 只被 application 装配层消费——用户调低软线后
// 装配层跟着动、回合内控制器仍按出厂比例压（同一名字两个来源）。
// 同理，seelebridge 构造 processor / 控制器时不传 MaxToolResultChars，
// max_tool_result_chars 就永远落在出厂默认 60000 上。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
)

// strictBudget 是 BudgetProvider 桩：只给账号窗口与最大输出。
type strictBudget struct {
	window int
	output int
}

func (b strictBudget) ContextTokens() int   { return b.window }
func (b strictBudget) MaxOutputTokens() int { return b.output }

// TestContextWindowPolicyUsesConfiguredRatios：四个比例真的进入阈值数字，
// 且与默认比例算出的结果**不相等**（相等就等于没消费配置）。
func TestContextWindowPolicyUsesConfiguredRatios(t *testing.T) {
	custom := Limits{
		ContextSafetyReserveDivisor: 4,
		ContextSoftPercent:          60,
		ContextHardPercent:          85,
		ContextTargetPercent:        45,
	}
	policy := NewContextWindowPolicy(100_000, 8_192, custom)

	// 预算 = 100_000 − 8_192 − 100_000/4 = 66_808（手工算，不叫代码自证）。
	const budget = 66_808
	if got := policy.Budget(); got != budget {
		t.Fatalf("预算 = %d，期望 %d（除数未按 context_safety_reserve_divisor=4 生效）", got, budget)
	}
	for _, want := range []struct {
		name string
		got  int
		want int
	}{
		{"soft", policy.SoftThreshold(), budget * 60 / 100},
		{"hard", policy.HardThreshold(), budget * 85 / 100},
		{"target", policy.TargetAfterCompaction(), budget * 45 / 100},
	} {
		if want.got != want.want {
			t.Fatalf("%s 阈值 = %d，期望 %d", want.name, want.got, want.want)
		}
	}

	// 与默认比例的差异必须存在：这是"配置被消费"的可证伪判据。
	def := NewContextWindowPolicy(100_000, 8_192, DefaultLimits())
	if policy.SoftThreshold() == def.SoftThreshold() {
		t.Fatalf("改 context_soft_percent 后软阈值不变（=%d）→ 配置没被消费", policy.SoftThreshold())
	}
	if policy.HardThreshold() == def.HardThreshold() || policy.TargetAfterCompaction() == def.TargetAfterCompaction() {
		t.Fatal("改 hard/target 比例后阈值不变 → 配置没被消费")
	}

	// 零值 Limits 回退默认比例：构造即归一。归一被摘掉时阈值会变成 0，
	// 后果是每一轮都被判成超限狂压上下文，所以这条必须钉住。
	zero := NewContextWindowPolicy(100_000, 8_192, Limits{})
	if zero.Budget() != def.Budget() || zero.SoftThreshold() != def.SoftThreshold() {
		t.Fatalf("零值 Limits 未回退默认：budget=%d soft=%d", zero.Budget(), zero.SoftThreshold())
	}
}

// TestControllerOversizedToolUsesConfiguredLimit：注入的
// limits.max_tool_result_chars 必须真的决定"这条工具结果算不算超大"。
func TestControllerOversizedToolUsesConfiguredLimit(t *testing.T) {
	limit := 100
	controller := &seelexContextController{opts: ControllerOptions{MaxToolResultChars: limit}}

	oversized := &seelectx.ToolResult{Raw: strings.Repeat("a", limit+1)}
	if !controller.oversizedTool(oversized) {
		t.Fatalf("%d 字节结果在 max_tool_result_chars=%d 下必须判为超大", limit+1, limit)
	}
	boundary := &seelectx.ToolResult{Raw: strings.Repeat("a", limit)}
	if controller.oversizedTool(boundary) {
		t.Fatalf("恰好 %d 字节不该判为超大（判定是严格大于）", limit)
	}

	// 同一份内容在未注入（回退出厂默认）下不算超大：证明上一条差异来自配置，
	// 不是 IsOversizedToolResult 本身恒真。
	uninjected := &seelexContextController{}
	if uninjected.oversizedTool(oversized) {
		t.Fatalf("出厂默认上限 %d 下 %d 字节不该判为超大", DefaultToolResultLimit(), limit+1)
	}
}
