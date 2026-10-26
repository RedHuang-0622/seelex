package contract

// RetainFloorTokens returns the percentage-derived lower bound of the context
// retention window. A non-positive percentage or budget disables the bound and
// returns zero; this is a pure cross-layer calculation, not a policy owner.
func RetainFloorTokens(percent, budgetTokens int) int {
	if percent <= 0 || budgetTokens <= 0 {
		return 0
	}
	return budgetTokens * percent / 100
}
