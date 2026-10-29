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
			Required:    []string{"writing or exporting a\n  Markdown report", "deliver once the planned verification and reporting stages are done"},
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
			Required:    []string{"Visible Intent Before Tools", "Before the first tool call in a distinct phase", "before the first mutation", "hidden internal plan JSON", "one uninterrupted\n  phase"},
		},
		{
			Name:        "fork-subagents-allow-isolated-work",
			Effort:      "high",
			UserRequest: "并行调研多个模块后汇总。",
			Expected:    "允许 fork_subagents 派发隔离子代理：每条有独立 Session/worktree、继承项目作用域与父证据，句柄经 bg_manage 取回，完成后 defer 单个 task_complete。",
			Required:    []string{"fork_subagents", "own Session and worktree", "bg_manage(op=fetch, handle)", "defer a single `task_complete`"},
		},
		{
			Name:        "visual-answer-uses-sandboxed-html-block",
			Effort:      "high",
			UserRequest: "把最近几次任务的耗时画成图给我看。",
			Expected:    "图表类回答用 seelex-html 围栏块渲染：块自包含、无网络、无应用访问；只做装饰或 markdown 表格已足够时不使用。",
			Required:    []string{"Rendered HTML for Visual Answers", "seelex-html", "source block", "clamped to 120–640px", "no network access"},
		},
		{
			Name:        "visual-answer-can-drive-the-conversation",
			Effort:      "high",
			UserRequest: "画一张可点的架构图，点节点就让你解释它。",
			Expected:    "块内交互默认留在沙箱内（形状而非标签承受点击、动画写 transform-box）；跨帧只能走白名单动作（ask-agent/fill-composer/copy-text/open-source），驱动会话需要围栏 interactive=1 与块内刚发生的真实手势，不许接在 onload/timer 上。",
			Required:    []string{"Interaction inside the block", "data-seelex-action", "ask-agent", "interactive=1", "pointer-events:none", "prefers-reduced-motion"},
		},
		{
			Name:        "visual-answer-uses-builtin-components-and-theme-tokens",
			Effort:      "high",
			UserRequest: "用卡片和表格把这几轮任务的耗时对比画出来。",
			Expected:    "块内直接用内置组件（article/button/table/progress 与 .sx-* 别名）并只引用 --sx-* 主题变量取色；警告/危险/成功/信息用各自的语义色，不跟品牌色走；不写死 #fff/#000 之类的固定底色。",
			Required:    []string{"Built-in components and theme tokens", ".sx-chip", "--sx-accent", "--sx-warn", "never the brand accent", "hardcoded `#fff` / `#000`"},
		},
		{
			Name:        "tasklist-vs-fork-distinction",
			Effort:      "high",
			UserRequest: "区分串行任务清单与并行子代理派发。",
			Expected:    "提示词区分 checklist（task_add + task_complete）与 fork_subagents（隔离子代理并行 + 句柄取回）；Plan 工具族已退役。",
			Required:    []string{"Checklists", "Subagent fork", "`task_add`", "`task_complete`"},
		},
		{
			Name:        "task-check-node-is-historical-only",
			Effort:      "high",
			UserRequest: "恢复一个仍有历史 Plan 结构的旧会话并逐节点打点。",
			Expected:    "task_check_node 只服务仍有历史 Plan 结构的恢复会话；新会话用 task_add 建立清单，不能创建 Plan。",
			Required:    []string{"`task_check_node` is only for resumed\nsessions that still have a historical Plan structure", "`task_add`", "new sessions cannot\ncreate one."},
		},
		{
			Name:        "long-command-goes-to-the-background-entry",
			Effort:      "high",
			UserRequest: "跑完整测试套件，跑的时候继续把审查做完。",
			Expected:    "分钟级命令走后台作业面（bash_bg 派发 + bg_manage 取回）；串行 bash 超预算的 timeout 被拒绝；作业面关闭时才串行并显式给 timeout。",
			Required:    []string{"Long-Running Commands", "Use `bash_bg` when:", "Do not use `bash_bg` when:", "timeout` above 5 minutes instead of running it", "work-table row title", "If `bash_bg` is not in your tool list"},
		},
	}
}

// ValidatePlanActHarness returns an actionable failure when prompt edits drop
// an execution-completion guarantee required by a harness scenario.
//
// 2026-10 提示词归一后，四个档位共用同一份 effort 正文，所以这里只渲染一次，
// 不再按 case.Effort 取不同资产——case.Effort 仍保留，作为"这条场景当初是针对
// 哪一档写的"记录，读的人（和人改提示词时）需要它。
func ValidatePlanActHarness() error {
	for _, test := range PlanActHarnessCases() {
		prompt := SystemInstructions() + "\n" + Effort()
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
