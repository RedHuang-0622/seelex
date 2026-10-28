package view_state

import "unicode/utf8"

// ToolResultBudgetDeps supplies the preview judgement, archive write and
// configured visible-snapshot character budget.
type ToolResultBudgetDeps struct {
	Limit   func() int
	Preview func(content string, limit int) (visible string, truncated bool)
	Archive func(name, content string) (resultRef string)
}

// ToolResultBudgetApplication owns the visible snapshot tool-result boundary.
type ToolResultBudgetApplication struct {
	deps ToolResultBudgetDeps
}

// NewToolResultBudget constructs the tool-result budget application.
func NewToolResultBudget(deps ToolResultBudgetDeps) *ToolResultBudgetApplication {
	return &ToolResultBudgetApplication{deps: deps}
}

// Bound returns the visible preview and optional durable result reference. The
// caller owns the session/view lock required by Archive.
func (application *ToolResultBudgetApplication) Bound(
	name, content string,
) (visible string, resultRef string, truncated bool, totalChars int) {
	visible, truncated = application.deps.Preview(content, application.deps.Limit())
	totalChars = utf8.RuneCountInString(content)
	if !truncated {
		return content, "", false, totalChars
	}
	if application.deps.Archive != nil {
		resultRef = application.deps.Archive(name, content)
	}
	return visible, resultRef, true, totalChars
}
