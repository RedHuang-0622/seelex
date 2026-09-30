package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

type runtimeWithContextLimits struct {
	window int
	output int
}

func (runtime runtimeWithContextLimits) ContextWindow() int   { return runtime.window }
func (runtime runtimeWithContextLimits) MaxOutputTokens() int { return runtime.output }

type legacyRuntime struct{}

func TestContextBudgetUsesRuntimeLimits(t *testing.T) {
	runtime := runtimeWithContextLimits{window: 200_000, output: 8_192}
	budget := ContextBudgetFor(runtime)
	if budget.Window != 200_000 || budget.OutputReserve != 8_192 || budget.SafetyReserve != 25_000 {
		t.Fatalf("runtime budget reserves = %+v", budget)
	}
	if budget.Budget != 166_808 || budget.TargetAfterCompaction != 133_446 {
		t.Fatalf("runtime budget = %+v", budget)
	}
}

func TestContextBudgetFallsBackForLegacyRuntime(t *testing.T) {
	if got, want := ContextBudgetFor(legacyRuntime{}), DefaultContextBudget(); got != want {
		t.Fatalf("fallback budget = %+v, want %+v", got, want)
	}
}

// TestContextBudgetRatiosComeFromLimits：压缩预算比例是配置项（seelex.yaml
// limits 段），不再是硬编码常量——调 context_hard_percent 必须改变硬/目标与
// 单条外置阈值，且默认值只来自 seelexctx.DefaultLimits（窗口/8、98%、80%、50%）。
// 报表口径与判据口径必须来自同一份数字，这个用例把它钉死。
//
// 2026-09-30 起只剩一条线（取消软线提前量，见 context_runtime 的折叠判据）：
// SoftThreshold 与 HardThreshold 同值——报告面因此不会出现"报表说已过软线、
// 系统却没折"的口径分裂；limits.context_soft_percent 不再被消费（保留键兼容旧配置）。
func TestContextBudgetRatiosComeFromLimits(t *testing.T) {
	previous := limits.Get()
	defer limits.Apply(previous)
	limits.Apply(seelexctx.DefaultLimits())

	base := ContextBudgetFor(runtimeWithContextLimits{window: 200_000, output: 8_192})
	if base.SafetyReserve != 25_000 || base.Budget != 166_808 {
		t.Fatalf("默认预算基数 = %+v", base)
	}
	if base.SoftThreshold != 163_471 || base.HardThreshold != 163_471 {
		t.Fatalf("默认阈值（软并入硬，两条线同值）= %+v", base)
	}
	if base.TargetAfterCompaction != 133_446 || base.SingleItemInputLimit != 83_404 {
		t.Fatalf("默认目标/单条外置阈值 = %+v", base)
	}

	limits.Apply(seelexctx.Limits{
		ContextSafetyReserveDivisor: 4,
		ContextSoftPercent:          50,
		ContextHardPercent:          80,
		ContextTargetPercent:        40,
		ContextSingleItemPercent:    25,
	})
	configured := ContextBudgetFor(runtimeWithContextLimits{window: 200_000, output: 8_192})
	if configured.SafetyReserve != 50_000 {
		t.Fatalf("安全预留未按 context_safety_reserve_divisor 生效: %+v", configured)
	}
	budget := 200_000 - 8_192 - 50_000
	if configured.Budget != budget ||
		configured.SoftThreshold != budget*80/100 ||
		configured.HardThreshold != budget*80/100 ||
		configured.TargetAfterCompaction != budget*40/100 ||
		configured.SingleItemInputLimit != budget*25/100 {
		t.Fatalf("配置比例未生效: %+v (预算 %d)", configured, budget)
	}

	// 非法比例（0/负/超 100）回退默认，不得把预算算成 0（那会让每轮都压缩）。
	limits.Apply(seelexctx.Limits{ContextSoftPercent: 0, ContextHardPercent: 200})
	fallback := ContextBudgetFor(runtimeWithContextLimits{window: 200_000, output: 8_192})
	if fallback.SoftThreshold != 163_471 || fallback.HardThreshold != 163_471 {
		t.Fatalf("非法比例未回退默认: %+v", fallback)
	}
}

func TestCalibratedTokenCounter_InitialFactor(t *testing.T) {
	c := NewCalibratedTokenCounter()
	if got := c.CountText("中文"); got != 2 {
		t.Fatalf("CountText(中文) = %d, want 2", got)
	}
	if got := c.CountText("abcd"); got != 1 {
		t.Fatalf("CountText(abcd) = %d, want 1", got)
	}
}

func TestCalibratedTokenCounter_ObserveAdjustsFactor(t *testing.T) {
	c := NewCalibratedTokenCounter()
	// actual/estimated = 1.5 → factor = 0.7*1 + 0.3*1.5 = 1.15
	c.Observe(100, 150)
	// base=1 → ceil(1*1.15)=2
	if got := c.CountText("abcd"); got != 2 {
		t.Fatalf("CountText after Observe = %d, want 2", got)
	}
}

func TestCalibratedTokenCounter_ObserveClamps(t *testing.T) {
	c := NewCalibratedTokenCounter()
	c.Observe(100, 1000) // ratio=10 → clamp 到 maxCalibrationFactor
	if got := c.CountText("abcd"); got > 3 {
		t.Fatalf("CountText must respect max factor, got %d", got)
	}
	c.Observe(100, 10) // ratio=0.1 → clamp 到 minCalibrationFactor
	if got := c.CountText("abcd"); got < 1 {
		t.Fatalf("CountText must respect min factor, got %d", got)
	}
}

func TestCalibratedTokenCounter_ObserveIgnoresNonPositive(t *testing.T) {
	c := NewCalibratedTokenCounter()
	c.Observe(0, 100)
	c.Observe(100, 0)
	if got := c.CountText("abcd"); got != 1 {
		t.Fatalf("non-positive usage must not change estimate, got %d", got)
	}
}
