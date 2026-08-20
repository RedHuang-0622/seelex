package core

import "testing"

type runtimeWithContextLimits struct {
	*fakeRuntime
	window int
	output int
}

func (runtime runtimeWithContextLimits) ContextWindow() int   { return runtime.window }
func (runtime runtimeWithContextLimits) MaxOutputTokens() int { return runtime.output }

func TestContextBudgetUsesRuntimeLimits(t *testing.T) {
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	budget := contextBudgetFor(runtime)
	if budget.Window != 200_000 || budget.OutputReserve != 8_192 || budget.SafetyReserve != 25_000 {
		t.Fatalf("runtime budget reserves = %+v", budget)
	}
	if budget.Budget != 166_808 || budget.TargetAfterCompaction != 100_084 {
		t.Fatalf("runtime budget = %+v", budget)
	}
}

func TestContextBudgetFallsBackForLegacyRuntime(t *testing.T) {
	if got, want := contextBudgetFor(&fakeRuntime{}), defaultContextBudget(); got != want {
		t.Fatalf("fallback budget = %+v, want %+v", got, want)
	}
}

func TestCalibratedTokenCounter_InitialFactor(t *testing.T) {
	c := newCalibratedTokenCounter()
	if got := c.CountText("中文"); got != 2 {
		t.Fatalf("CountText(中文) = %d, want 2", got)
	}
	if got := c.CountText("abcd"); got != 1 {
		t.Fatalf("CountText(abcd) = %d, want 1", got)
	}
}

func TestCalibratedTokenCounter_ObserveAdjustsFactor(t *testing.T) {
	c := newCalibratedTokenCounter()
	// actual/estimated = 1.5 → factor = 0.7*1 + 0.3*1.5 = 1.15
	c.Observe(100, 150)
	// base=1 → ceil(1*1.15)=2
	if got := c.CountText("abcd"); got != 2 {
		t.Fatalf("CountText after Observe = %d, want 2", got)
	}
}

func TestCalibratedTokenCounter_ObserveClamps(t *testing.T) {
	c := newCalibratedTokenCounter()
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
	c := newCalibratedTokenCounter()
	c.Observe(0, 100)
	c.Observe(100, 0)
	if got := c.CountText("abcd"); got != 1 {
		t.Fatalf("non-positive usage must not change estimate, got %d", got)
	}
}
