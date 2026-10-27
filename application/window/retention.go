package window

import (
	taskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	limit "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract"
)

// RetainedReadTailBudget returns the retained-prefix token budget used when
// cold restore reads the transcript/history tail.
func RetainedReadTailBudget(runtime any) int {
	budget := taskcontext.ContextBudgetFor(runtime)
	return Current().RetainedContextTokensWithFloor(budget.Window, budget.Window, RetainFloorTokens(budget))
}

// RetainFloorTokens returns the configured retained-floor token count.
func RetainFloorTokens(budget taskcontext.ContextBudget) int {
	return contract.RetainFloorTokens(limit.Get().ContextRetainFloorPercent, budget.Budget)
}

// ValidateRetainWindow validates the window/limits relation at startup.
func ValidateRetainWindow(config WindowConfig, limits limit.Limits, windowTokens int) error {
	budget := 0
	if windowTokens > 0 {
		budget = taskcontext.ContextBudgetFor(retainWindowLimits{
			window: windowTokens, output: limits.OutputReserveTokens,
		}).Budget
	}
	return config.ValidateRetainWindow(contract.RetainFloorTokens(limits.ContextRetainFloorPercent, budget))
}

type retainWindowLimits struct {
	window int
	output int
}

func (r retainWindowLimits) ContextWindow() int   { return r.window }
func (r retainWindowLimits) MaxOutputTokens() int { return r.output }
