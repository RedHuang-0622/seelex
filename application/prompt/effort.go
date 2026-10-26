package prompt

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/limit/effort"
	"github.com/RedHuang-0622/seelex/kernel/limit/loop"
	promptassets "github.com/RedHuang-0622/seelex/kernel/prompt/assets"
)

// effort.go 是 effort 档位的**宿主侧管理器**：它把档位策略应用到 PromptStack
// 与引擎（SetMaxLoops）。档位数字与策略表本身在 `kernel/limit/effort`
// （plan §2.8 的落点）——本文件只保留应用动作与既有调用点的门面。

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

// ReActBudget 是"一次用户请求的工具预算"的类型别名：数字与取数规则只有一份，
// 在 `kernel/limit/loop`（判据层）。
type ReActBudget = loop.Budget

// DefaultEffortLevel 是进程默认档位：策略事实在 `kernel/limit/effort`。
const DefaultEffortLevel = effort.DefaultLevel

// ReasoningEffortFor returns the thinking-strength value for an effort level.
func ReasoningEffortFor(level string) string { return effort.ReasoningEffortFor(level) }

// MaxLoops returns the engine loop limit for an effort level.
func MaxLoops(level string) int { return loop.MaxLoops(level) }

// ReActBudgetFor returns an immutable execution budget for an effort level.
func ReActBudgetFor(level string) ReActBudget { return loop.BudgetFor(level) }

// SubagentPolicyFor returns the plan constraints for an effort level.
func SubagentPolicyFor(level string) dto.SubagentPolicy { return effort.SubagentPolicyFor(level) }

// ValidEffortLevels 返回所有有效 effort 等级。
func ValidEffortLevels() []string { return effort.Levels() }

// SubagentPolicy returns the constraints for the manager's current effort level.
func (m *EffortManager) SubagentPolicy() dto.SubagentPolicy {
	return SubagentPolicyFor(m.Current())
}

// NewEffortManager 创建 Effort 管理器。
func NewEffortManager(ps *PromptStack, eng interface {
	SetMaxLoops(int)
	SetSystemPrompt(string)
}) *EffortManager {
	return &EffortManager{
		promptStack: ps,
		engine:      eng,
		current:     DefaultEffortLevel,
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
	profile, ok := effort.For(level)
	if !ok {
		return fmt.Errorf("invalid effort level %q, valid: %v", level, effort.Levels())
	}

	m.promptStack.ClearKind("effort")
	// 四个档位共用同一份行为指令（2026-10 起）：档位差异只由策略读数表达。
	m.promptStack.Push("effort", level, promptassets.Effort())
	m.engine.SetMaxLoops(profile.Loops.MaxLoops)
	m.current = level
	return nil
}

// Current 返回当前 effort 等级。
func (m *EffortManager) Current() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// Cycle 循环切换到下一个 effort 等级。
func (m *EffortManager) Cycle() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := effort.OrderedLevels[0]
	for i, l := range effort.OrderedLevels {
		if l == m.current && i+1 < len(effort.OrderedLevels) {
			next = effort.OrderedLevels[i+1]
			break
		}
	}
	return next, m.applyLocked(next)
}
