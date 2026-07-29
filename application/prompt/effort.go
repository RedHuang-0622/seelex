package prompt

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// EffortManager 管理 Effort 等级及对应行为。
// Effort 通过 PromptStack 的 effort 层注入行为指令，
// 以及通过 engine.SetMaxLoops 控制循环轮次。
type EffortManager struct {
	mu          sync.Mutex
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
- For every non-trivial task, your first action MUST be a plan_load tool call, unless the runtime supplies an authoritative preflight WorkPlan for this request. Do not substitute a prose outline or a final answer for that call.
- When the current user context begins with the seelex:plan-context:v1 authority marker, planning is already complete. Never call plan_load or plan_clear in that turn, even if the user asks to create a plan; use the loaded WorkPlan or plan_run. Explicit recovery replan remains available after a plan_run failure.
- Load the plan before any execution tool. Call plan_run only when the task requires executing its nodes.
- The plan must have at most 4 nodes and be one serial chain. These constraints are runtime-enforced.
- Keep responses concise. Use tools only when necessary.
- Retry once on tool failure.`),

	"high": strings.TrimSpace(`
You are in high-effort mode.
- For every non-trivial task, your first action MUST be a plan_load tool call, unless the runtime supplies an authoritative preflight WorkPlan for this request. Do not substitute a prose outline or a final answer for that call.
- When the current user context begins with the seelex:plan-context:v1 authority marker, planning is already complete. Never call plan_load or plan_clear in that turn, even if the user asks to create a plan; use the loaded WorkPlan or plan_run. Explicit recovery replan remains available after a plan_run failure.
- Load the plan before any execution tool. Call plan_run only when the task requires executing its nodes.
- Independent nodes may run in parallel, but the runtime limits plan concurrency to 3.
- On tool failure, attempt auto-fix and retry up to 3 times.
- Verify results after each change (compile/test).
- Use ask_approve for destructive operations.
- You can switch plugins via switch_plugin when needed.`),

	"max": strings.TrimSpace(`
You are in max-effort mode.
- For every non-trivial task, your first action MUST be a plan_load tool call, unless the runtime supplies an authoritative preflight WorkPlan for this request. Do not substitute a prose outline or a final answer for that call.
- When the current user context begins with the seelex:plan-context:v1 authority marker, planning is already complete. Never call plan_load or plan_clear in that turn, even if the user asks to create a plan; use the loaded WorkPlan or plan_run. Explicit recovery replan remains available after a plan_run failure.
- Load the WorkPlan before any execution tool. Call plan_run only when the task requires executing its nodes.
- Use Fork for parallel sub-agents when tasks are independent.
- All independent plan nodes may run in parallel; the runtime does not impose a per-plan concurrency cap.
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

// MaxLoops returns the engine loop limit for an effort level.
func MaxLoops(level string) int { return effortLoops[level] }

// PlanningPolicy returns the hard runtime constraints for an effort level.
// Lite leaves planning optional. Max uses the loaded plan's node count as its
// concurrency cap so all currently runnable nodes can start together.
func PlanningPolicy(level string) seelebridge.PlanPolicy {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "medium":
		return seelebridge.PlanPolicy{Effort: "medium", RequirePlan: true, MaxNodes: 4, RequireSerial: true, MaxForkConcurrency: 1}
	case "high":
		return seelebridge.PlanPolicy{Effort: "high", RequirePlan: true, MaxForkConcurrency: 3}
	case "max":
		return seelebridge.PlanPolicy{Effort: "max", RequirePlan: true}
	default:
		return seelebridge.PlanPolicy{Effort: "lite"}
	}
}

// PlanPolicy returns the constraints for the manager's current effort level.
func (m *EffortManager) PlanPolicy() seelebridge.PlanPolicy {
	return PlanningPolicy(m.Current())
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

// Apply 切换 effort 等级并更新 effort prompt 层与 MaxLoops。
// 调用方在需要时用 PromptStack.Render 重绘完整 system prompt。
func (m *EffortManager) Apply(level string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyLocked(level)
}

func (m *EffortManager) applyLocked(level string) error {
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
	return nil
}

// Current 返回当前 effort 等级。
func (m *EffortManager) Current() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

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
	m.mu.Lock()
	defer m.mu.Unlock()
	next := orderedLevels[0]
	for i, l := range orderedLevels {
		if l == m.current && i+1 < len(orderedLevels) {
			next = orderedLevels[i+1]
			break
		}
	}
	return next, m.applyLocked(next)
}
