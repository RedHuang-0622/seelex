package prompt

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
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

// effortPrompts maps user-selected levels to versioned prompt assets. Prompt
// prose belongs in internal/promptassets, not in application code.
var effortPrompts = map[string]string{
	"lite":   promptassets.Effort("lite"),
	"medium": promptassets.Effort("medium"),
	"high":   promptassets.Effort("high"),
	"max":    promptassets.Effort("max"),
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

	if prompt, ok := effortPrompts[level]; ok {
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
