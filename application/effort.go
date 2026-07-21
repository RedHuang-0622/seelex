package application

import (
	"fmt"
	"strings"
)

// EffortManager 管理 Effort 等级及对应行为。
// Effort 通过 PromptStack 的 effort 层注入行为指令，
// 以及通过 engine.SetMaxLoops 控制循环轮次。
type EffortManager struct {
	promptStack *PromptStack
	engine      interface {
		SetMaxLoops(int)
		SetSystemPrompt(string)
	}
	current string
}

// effortPrompts 存储各等级的行为指令。
var effortPrompts = map[string]string{
	"lite": "", // lite 不注入 effort 层，仅靠 MaxLoops=0 约束

	"medium": strings.TrimSpace(`
You are in medium-effort mode.
- For multi-step tasks, use plan_load to define a plan, then plan_run.
- Plan node concurrency: maximum 2 nodes may run in parallel.
- Keep responses concise. Use tools only when necessary.
- Retry once on tool failure.`),

	"high": strings.TrimSpace(`
You are in high-effort mode.
- For multi-step tasks, use plan_load to define a plan, then plan_run.
- Plan node concurrency: maximum 4 nodes may run in parallel.
- On tool failure, attempt auto-fix and retry up to 3 times.
- Verify results after each change (compile/test).
- Use ask_approve for destructive operations.
- You can switch plugins via switch_plugin when needed.`),

	"max": strings.TrimSpace(`
You are in max-effort mode.
- Always plan before acting. Use WorkPlan for complex tasks.
- Use Fork for parallel sub-agents when tasks are independent.
- Plan node concurrency: unlimited — all independent plan nodes may run in parallel.
- On tool failure, retry with alternative approach up to 5 times.
- Cross-verify results with multiple methods.
- Use worktrees for isolated experiments.
- Record key decisions and findings for review.`),
}

// effortLoops 存储各等级的 MaxLoops 值。
var effortLoops = map[string]int{
	"lite":   20,
	"medium": 64,
	"high":   512,
	"max":    1024,
}

// NewEffortManager 创建 Effort 管理器。
func NewEffortManager(ps *PromptStack, eng interface {
	SetMaxLoops(int)
	SetSystemPrompt(string)
}) *EffortManager {
	return &EffortManager{
		promptStack: ps,
		engine:      eng,
		current:     "high", // 默认 high
	}
}

// Apply 切换 effort 等级。
// 更新 effort prompt 层、MaxLoops，并重绘完整 system prompt。
func (m *EffortManager) Apply(level string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	if _, ok := effortPrompts[level]; !ok {
		valid := make([]string, 0, len(effortPrompts))
		for k := range effortPrompts {
			valid = append(valid, k)
		}
		return fmt.Errorf("invalid effort level %q, valid: %v", level, valid)
	}

	m.promptStack.ClearKind("effort")

	if prompt, ok := effortPrompts[level]; ok && prompt != "" {
		m.promptStack.Push("effort", level, prompt)
	}
	if loops, ok := effortLoops[level]; ok {
		m.engine.SetMaxLoops(loops)
	}
	m.current = level
	m.engine.SetSystemPrompt(m.promptStack.Render())
	return nil
}

// Current 返回当前 effort 等级。
func (m *EffortManager) Current() string { return m.current }

// ValidLevels 返回所有有效 effort 等级。
func ValidEffortLevels() []string {
	levels := make([]string, 0, len(effortPrompts))
	for k := range effortPrompts {
		levels = append(levels, k)
	}
	return levels
}

// orderedLevels 是 effort 循环顺序。
var orderedLevels = []string{"lite", "medium", "high", "max"}

// Cycle 循环切换到下一个 effort 等级。
func (m *EffortManager) Cycle() (string, error) {
	next := orderedLevels[0]
	for i, l := range orderedLevels {
		if l == m.current && i+1 < len(orderedLevels) {
			next = orderedLevels[i+1]
			break
		}
	}
	return next, m.Apply(next)
}
