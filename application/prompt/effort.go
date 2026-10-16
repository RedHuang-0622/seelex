package prompt

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/backend/limit/loop"
	"github.com/RedHuang-0622/seelex/contract/dto"
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

// ReActBudget 是"一次用户请求的工具预算"的类型别名：数字与取数规则只有一份，
// 在 `backend/limit/loop`（判据层）。这里保留这个名字只是为了既有调用方的
// 类型身份不变（别名不产生新类型）。
type ReActBudget = loop.Budget

// effortProfile keeps every effort-specific behavior in one entry so a new
// level cannot accidentally receive a prompt without its execution budget.
type effortProfile struct {
	// level 是该档在 effortProfiles 里的键，也是对外露出的档位名与 PlanPolicy
	// 的 Effort 字段（lite/medium/high/max）。**提示词已不再与它绑定**：2026-10
	// 起四个档位共用一份 assets/effort/system.md（见 promptassets.Effort 的注释），
	// 档位差异只由 reasoningEffort / maxLoops / planPolicy 表达。
	// Prompt() 仍然**惰性取正文**：effortProfiles 是包级变量、在包初始化期构造，
	// 早于 main 设定外部提示词目录（config/prompt）；初始化期就把正文取出来会把
	// 内嵌词定死，外部覆盖随之失效。
	level string
	// loops 是这一档在循环维度上的读数（轮数 + 工具预算）。**数字来自
	// `backend/limit/loop` 的唯一档位表**，这里只做登记，不抄数字——
	// 抄一遍就会出现"改了档位表、拉条读数没变"的漂移。
	loops      loop.Profile
	planPolicy dto.PlanPolicy
	// reasoningEffort is the thinking-strength value sent to the provider (wire
	// vocabulary: low/medium/high/max). Loop counts and budgets stay as they are;
	// this field only expresses "how deep to think".
	reasoningEffort string
}

// Prompt 返回**统一**的 effort 行为指令正文（外部覆盖目录优先、内嵌兜底）。
// 四个档位拿到的是同一份：档位差异不再由提示词表达。
func (p effortProfile) Prompt() string { return promptassets.Effort() }

// effortProfiles maps user-selected levels to versioned prompt assets and
// hard execution budgets. Prompt prose belongs in internal/promptassets, not
// in application code; 循环数字（轮数与工具预算）属于 `backend/limit/loop`。
//
// 每条记录仍然是**一个档位一条**：提示词、planPolicy、reasoningEffort、循环读数
// 一起登记，新增档位时不会只拿到其中一半（这是刻意的，别把它拆成两处登记）。
var effortProfiles = map[string]effortProfile{
	"lite": {
		level:           "lite",
		loops:           mustLoopProfile("lite"),
		planPolicy:      dto.PlanPolicy{Effort: "lite"},
		reasoningEffort: "low",
	},
	"medium": {
		level: "medium",
		// MaxForkConcurrency 有意保持 0：subagent 只是角色 + 模型供应商，
		// 并发不再由 effort 档限制（PolicyConcurrency 回退为“全部当前可
		// 运行节点同时执行”）。medium 只保留节点数与串行链约束。
		planPolicy:      dto.PlanPolicy{Effort: "medium", MaxNodes: 4, RequireSerial: true},
		reasoningEffort: "medium",
		loops:           mustLoopProfile("medium"),
	},
	"high": {
		level: "high",
		// 同上：effort 不再携带子代理并发上限，DAG 中所有当前可运行
		// agent 节点默认同时执行。
		planPolicy:      dto.PlanPolicy{Effort: "high", MaxNodeLoops: 48},
		reasoningEffort: "high",
		loops:           mustLoopProfile("high"),
	},
	"max": {
		level:           "max",
		planPolicy:      dto.PlanPolicy{Effort: "max", MaxNodeLoops: 96},
		reasoningEffort: "max",
		loops:           mustLoopProfile("max"),
	},
}

// mustLoopProfile 取档位的循环读数。档位表是包级静态表，缺条目属于编程错误
// （改了这里却漏了 `backend/limit/loop` 的档位），在初始化期就报出来，
// 不让"某个档位没有预算"这种状态活到运行期。
func mustLoopProfile(level string) loop.Profile {
	profile, ok := loop.For(level)
	if !ok {
		panic("prompt: effort profile " + level + " has no loop profile in backend/limit/loop")
	}
	return profile
}

func effortProfileFor(level string) (effortProfile, bool) {
	profile, ok := effortProfiles[strings.ToLower(strings.TrimSpace(level))]
	return profile, ok
}

// DefaultEffortLevel 是进程默认档位：新会话（未在自己的 Unit 里选过档位）回退到它，
// 也是"未声明执行参数"一类的缺省来源（定时任务的思考强度/循环次数默认值 = 这一档的
// 两条读数）。**别处要默认值时走入口读数**：思考强度读 `ReasoningEffortFor`、
// 循环数字读 `MaxLoops`（后者转发到判据层 `backend/limit/loop`），
// 不许把档位名或数字再抄一遍。
const DefaultEffortLevel = "high"

// ReasoningEffortFor returns the thinking-strength value for an effort level.
// Unknown levels return "" - an empty value stays off the wire and the provider
// applies its own default; we never guess a value.
func ReasoningEffortFor(level string) string {
	profile, ok := effortProfileFor(level)
	if !ok {
		return ""
	}
	return profile.reasoningEffort
}

// MaxLoops returns the engine loop limit for an effort level.
//
// 数字只有一份，在 `backend/limit/loop`；这里只是既有调用方的入口。
func MaxLoops(level string) int { return loop.MaxLoops(level) }

// ReActBudgetFor returns an immutable execution budget for an effort level.
//
// 未识别档位兜底到 lite，口径与迁移前一致（见 `loop.BudgetFor`）。
func ReActBudgetFor(level string) ReActBudget { return loop.BudgetFor(level) }

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
		current:     DefaultEffortLevel, // 默认档位（见 DefaultEffortLevel）
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

	m.promptStack.Push("effort", level, profile.Prompt())
	m.engine.SetMaxLoops(profile.loops.MaxLoops)
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
