package prompt

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
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

// ReActBudget bounds one user request. MaxToolRounds counts completed ReAct
// iterations that made tool calls; MaxToolCalls counts individual tool calls.
type ReActBudget struct {
	MaxToolRounds       int
	MaxToolCalls        int
	MaxNoProgressRounds int
}

// effortProfile keeps every effort-specific behavior in one entry so a new
// level cannot accidentally receive a prompt without its execution budget.
type effortProfile struct {
	prompt     string
	maxLoops   int
	planPolicy dto.PlanPolicy
	budget     ReActBudget
}

// effortProfiles maps user-selected levels to versioned prompt assets and
// hard execution budgets. Prompt prose belongs in internal/promptassets, not
// in application code.
var effortProfiles = map[string]effortProfile{
	"lite": {
		prompt:     promptassets.Effort("lite"),
		maxLoops:   15,
		planPolicy: dto.PlanPolicy{Effort: "lite"},
		budget:     ReActBudget{MaxToolRounds: 15, MaxToolCalls: 30, MaxNoProgressRounds: 6},
	},
	"medium": {
		prompt:   promptassets.Effort("medium"),
		maxLoops: 48,
		// MaxForkConcurrency 有意保持 0：subagent 只是角色 + 模型供应商，
		// 并发不再由 effort 档限制（PolicyConcurrency 回退为“全部当前可
		// 运行节点同时执行”）。medium 只保留节点数与串行链约束。
		planPolicy: dto.PlanPolicy{Effort: "medium", MaxNodes: 4, RequireSerial: true},
		budget:     ReActBudget{MaxToolRounds: 48, MaxToolCalls: 96, MaxNoProgressRounds: 10},
	},
	"high": {
		prompt:   promptassets.Effort("high"),
		maxLoops: 384,
		// 同上：effort 不再携带子代理并发上限，DAG 中所有当前可运行
		// agent 节点默认同时执行。
		planPolicy: dto.PlanPolicy{Effort: "high", MaxNodeLoops: 48},
		budget:     ReActBudget{MaxToolRounds: 384, MaxToolCalls: 768, MaxNoProgressRounds: 24},
	},
	"max": {
		prompt:     promptassets.Effort("max"),
		maxLoops:   768,
		planPolicy: dto.PlanPolicy{Effort: "max", MaxNodeLoops: 96},
		budget:     ReActBudget{MaxToolRounds: 768, MaxToolCalls: 1536, MaxNoProgressRounds: 48},
	},
}

func effortProfileFor(level string) (effortProfile, bool) {
	profile, ok := effortProfiles[strings.ToLower(strings.TrimSpace(level))]
	return profile, ok
}

// MaxLoops returns the engine loop limit for an effort level.
func MaxLoops(level string) int {
	profile, ok := effortProfileFor(level)
	if !ok {
		return 0
	}
	return profile.maxLoops
}

// ReActBudgetFor returns an immutable execution budget for an effort level.
func ReActBudgetFor(level string) ReActBudget {
	profile, ok := effortProfileFor(level)
	if !ok {
		profile = effortProfiles["lite"]
	}
	return profile.budget
}

// PlanningPolicy returns the hard runtime constraints for an optional
// plan_load at an effort level. It never makes planning a chat-entry gate.
// Effort profiles no longer carry MaxForkConcurrency (removed 2026-09-07):
// subagents are just a role plus a model provider, so fork_subagents and
// plan_run let every currently runnable node start together (PolicyConcurrency
// falls back to the loaded node count). Medium keeps its four-node serial
// chain constraint; MaxNodeLoops caps stay effort-specific.
func PlanningPolicy(level string) dto.PlanPolicy {
	profile, ok := effortProfileFor(level)
	if !ok {
		profile = effortProfiles["lite"]
	}
	return profile.planPolicy
}

// PlanPolicy returns the constraints for the manager's current effort level.
func (m *EffortManager) PlanPolicy() dto.PlanPolicy {
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
	profile, ok := effortProfileFor(level)
	if !ok {
		valid := make([]string, 0, len(effortProfiles))
		for k := range effortProfiles {
			valid = append(valid, k)
		}
		return fmt.Errorf("invalid effort level %q, valid: %v", level, valid)
	}

	m.promptStack.ClearKind("effort")

	m.promptStack.Push("effort", level, profile.prompt)
	m.engine.SetMaxLoops(profile.maxLoops)
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
	levels := make([]string, 0, len(effortProfiles))
	for k := range effortProfiles {
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
