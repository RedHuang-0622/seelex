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
