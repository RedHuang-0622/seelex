package promptassets

import (
	"fmt"
	"strings"
)

// PlanActHarnessCase is a deterministic prompt-regression scenario. It does
// not simulate a model; it locks the operational clauses that prevent a
// completed Plan from turning into an open-ended review loop.
type PlanActHarnessCase struct {
	Name        string
	Effort      string
	UserRequest string
	Expected    string
	Required    []string
}

// PlanActHarnessCases documents positive and negative completion scenarios
// against the rendered system and effort instructions.
func PlanActHarnessCases() []PlanActHarnessCase {
	return []PlanActHarnessCase{
		{
			Name:        "high-delivers-after-verification",
			Effort:      "high",
			UserRequest: "审查当前实现，验证结论，然后给出报告。",
			Expected:    "完成计划中的验证和报告后立即交付，不额外开启审查。",
			Required:    []string{"Verification is one bounded stage", "delivery point"},
		},
		{
			Name:        "max-allows-markdown-export-before-final",
			Effort:      "max",
			UserRequest: "完成审查后导出 Markdown 报告。",
			Expected:    "允许写入或导出报告，再给出最终答复。",
			Required:    []string{"writing or exporting a\n  Markdown report", "deliver after the planned cross-check"},
		},
		{
			Name:        "medium-rejects-unplanned-second-check",
			Effort:      "medium",
			UserRequest: "按计划检查并报告。",
			Expected:    "已验证阶段不得无故再做第二轮验证。",
			Required:    []string{"Do not add an unplanned second verification pass", "serial chain and then\nreported"},
		},
		{
			Name:        "terminal-protocol-converges-after-bounded-check",
			Effort:      "high",
			UserRequest: "验证一次后给出最终结论。",
			Expected:    "完成、需要用户决策或失败必须具有显式终态，不得无限调查。",
			Required:    []string{"call `task_complete`", "call `task_needs_user_decision`", "call `task_failed`", "rather than\nanother open-ended investigation"},
		},
	}
}

// ValidatePlanActHarness returns an actionable failure when prompt edits drop
// an execution-completion guarantee required by a harness scenario.
func ValidatePlanActHarness() error {
	for _, test := range PlanActHarnessCases() {
		prompt := SystemInstructions() + "\n" + Effort(test.Effort)
		for _, required := range test.Required {
			if !containsPromptClause(prompt, required) {
				return fmt.Errorf("prompt harness %q (%s) missing %q", test.Name, test.Effort, required)
			}
		}
	}
	return nil
}

func containsPromptClause(prompt, clause string) bool {
	return len(clause) == 0 || strings.Contains(prompt, clause)
}
