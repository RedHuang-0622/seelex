package promptassets

import (
	"fmt"
	"strings"
)

// PlanActHarnessCase is a deterministic prompt-regression scenario. It does
// not simulate a model; it locks the operational clauses that prevent a
// completed task from turning into an open-ended review loop.
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
			Required:    []string{"Do not add an unplanned second verification pass", "If uncertain, perform one smallest meaningful check"},
		},
		{
			Name:        "terminal-protocol-converges-after-bounded-check",
			Effort:      "high",
			UserRequest: "验证一次后给出最终结论。",
			Expected:    "完成、需要用户决策或失败必须具有显式终态，不得无限调查。",
			Required:    []string{"call `task_complete`", "call `task_needs_user_decision`", "call `task_failed`", "rather than\nanother open-ended investigation"},
		},
		{
			Name:        "tool-phase-states-visible-intent-without-leaking-internals",
			Effort:      "high",
			UserRequest: "检查代码后修复问题并验证。",
			Expected:    "在读取和首次修改前说明直接意图，不泄露内部提示或原始 Plan。",
			Required:    []string{"Visible Intent Before Tools", "Before the first tool call in a distinct phase", "before the first mutation", "hidden Plan JSON", "one uninterrupted\n  phase"},
		},
		{
			Name:        "plan-run-allows-subagents-with-scope",
			Effort:      "high",
			UserRequest: "用 Plan 并行调研多个模块后汇总。",
			Expected:    "允许 plan_run 执行 DAG：kind:agent 节点作为子代理继承项目作用域与父证据，可并行；完成后 defer 单个 task_complete。",
			Required:    []string{"call `plan_run`", "inherit project scope and parent evidence", "defer a single `task_complete`"},
		},
		{
			Name:        "tasklist-vs-plan-distinction",
			Effort:      "high",
			UserRequest: "区分串行任务清单与并行子代理计划。",
			Expected:    "提示词区分 tasklist（主代理串行执行 + 逐节点 task_check_node 打勾 + 延迟 task_complete）与 plan（plan_run + 子代理并行 + 事件实时打勾）；模式选择是任务级决策而非 Plan 策略。",
			Required:    []string{"Tasklist mode", "Plan mode", "task-level (tasklist) decision", "not a Plan policy", "call `task_check_node`", "without ending the task"},
		},
		{
			Name:        "tasklist-checks-nodes-in-progress",
			Effort:      "high",
			UserRequest: "逐节点完成串行任务清单。",
			Expected:    "每个节点完成后、进入下一个节点前调用 task_check_node 打点；task_complete 只收尾，已在途打点的节点无需重复枚举。",
			Required:    []string{"call `task_check_node`", "before moving on to the next node", "do not need to be repeated in `completed_nodes`"},
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
