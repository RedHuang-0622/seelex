package node

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/skill"
)

// SessionPort 是 Coordinator 需要的子代理会话注册表面（由根包注入
// subagentsession.SubagentSessions，接口化避免 node→session→fork 测试环）。
type SessionPort interface {
	Register(nodeID string, sess *frameworkSession.Session, goal string)
	Unregister(nodeID string) *snapshot.ContextSnapshot
	Conversation(nodeID string) ([]types.Message, bool)
	ContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool)
	ToolResultArchiverFor(nodeID string) *seelexctx.InMemoryToolResultArchiver
	ToolResult(nodeID, ref string) (string, bool)
}

// TreePort 是 Coordinator 需要的 fork 子代理树表面。
type TreePort interface {
	NoteSession(nodeID string, sess *frameworkSession.Session)
	MarkRunning(nodeID string)
	NoteSnapshot(nodeID string, snap *snapshot.ContextSnapshot)
	CompleteSubagentNode(nodeID, summary string, runErr error)
}

// TaskPort 是 Coordinator 需要的 task 注册表表面（worktable 生命周期打点）。
type TaskPort interface {
	SetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)
}

// CoordinatorDeps 是 node.Coordinator 的运行时依赖：跨域协作一律走闭包或
// 接口注入，域内不依赖 seelebridge 根包。
type CoordinatorDeps struct {
	Sessions        SessionPort
	Tree            TreePort
	Tasks           TaskPort
	Plan            *plan.Executor
	Evidence        func() *snapshot.ContextSnapshot
	Skills          *skill.Registry
	Limits          seelexctx.Limits
	GoalSkillActive func() bool
	AccountLimits   func() config.AccountLimits
	InheritedBlocks func() []seelectx.PromptBlock
	RelatedMemory   func(ctx context.Context, query string) []seelectx.PromptBlock
}

// Coordinator 收拢原 Runtime 侧节点门面：会话注册、fork 树终态、task 打点、
// plan 阶段事件、节点 PromptBlocks 与预算、skill 目录。Runtime 只装配它，
// 公开端口经 ports.go 委托。
type Coordinator struct {
	deps      CoordinatorDeps
	skills    *skill.Registry
	startedMu sync.Mutex
	started   map[string]struct{}
}

// NewCoordinator 构造节点协调器。
func NewCoordinator(deps CoordinatorDeps) *Coordinator {
	return &Coordinator{
		deps:    deps,
		skills:  deps.Skills,
		started: make(map[string]struct{}),
	}
}

// RegisterSession 注册运行中的子代理会话（详情查询数据面）。
func (c *Coordinator) RegisterSession(nodeID string, sess *frameworkSession.Session, goal string) {
	if c == nil || c.deps.Sessions == nil || c.deps.Tree == nil {
		return
	}
	c.deps.Sessions.Register(nodeID, sess, goal)
	c.deps.Tree.NoteSession(nodeID, sess)
}

// UnregisterSession 结束注册并保留最后快照（节点结束后详情仍可看）。
func (c *Coordinator) UnregisterSession(nodeID string) {
	if c == nil || c.deps.Sessions == nil || c.deps.Tree == nil {
		return
	}
	if snap := c.deps.Sessions.Unregister(nodeID); snap != nil {
		c.deps.Tree.NoteSnapshot(nodeID, snap)
	}
}

// CompleteSubagentNode 写入 fork 子代理树节点终态，并同步 task 注册表
// （B5：生命周期即打点源；非 fork 节点注册表无记录 → 忽略）。
func (c *Coordinator) CompleteSubagentNode(nodeID, summary string, err error) {
	if c == nil || c.deps.Tree == nil {
		return
	}
	c.deps.Tree.CompleteSubagentNode(nodeID, summary, err)
	if c.deps.Tasks != nil {
		status := dto.TaskCompleted
		if err != nil {
			status = dto.TaskFailed
		}
		_, _ = c.deps.Tasks.SetStatus("subagent:"+nodeID, status, summary)
	}
}

// Conversation 返回节点子代理的会话记录：运行中 → 子会话 History（实时）；
// 已结束 → 最后快照。只读子代理 actor，绝不触碰主会话。
func (c *Coordinator) Conversation(nodeID string) ([]types.Message, bool) {
	if c == nil || c.deps.Sessions == nil {
		return nil, false
	}
	return c.deps.Sessions.Conversation(nodeID)
}

// ContextSnapshot 返回节点子代理的结构化上下文快照（详情弹窗"上下文"标签）。
func (c *Coordinator) ContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if c == nil || c.deps.Sessions == nil {
		return nil, false
	}
	return c.deps.Sessions.ContextSnapshot(nodeID)
}

// ToolResultArchiverFor 返回节点专属工具结果归档器（惰性创建；同一节点跨
// plan_run 复用，直到被下一次 fork 覆盖——与 nodeContextSnapshots 同生命周期）。
func (c *Coordinator) ToolResultArchiverFor(nodeID string) *seelexctx.InMemoryToolResultArchiver {
	if c == nil || c.deps.Sessions == nil {
		return nil
	}
	return c.deps.Sessions.ToolResultArchiverFor(nodeID)
}

// ToolResult 读回节点子代理的工具结果原始内容（ref 必须带 node:<nodeID>: 前缀）。
// 只读节点归档器，安全。返回 (内容, 是否存在)。
func (c *Coordinator) ToolResult(nodeID, ref string) (string, bool) {
	if c == nil || c.deps.Sessions == nil || nodeID == "" || ref == "" {
		return "", false
	}
	return c.deps.Sessions.ToolResult(nodeID, ref)
}

// AppendPhase 把节点生命周期阶段事件写入 plan 执行器（worktree/running 等）。
func (c *Coordinator) AppendPhase(ctx context.Context, nodeID, status string) {
	if c == nil || c.deps.Plan == nil {
		return
	}
	c.deps.Plan.AppendPhase(ctx, nodeID, status)
}

// ParentEvidence 返回 Runtime 持有的父证据快照（子代理可读）。
func (c *Coordinator) ParentEvidence() *snapshot.ContextSnapshot {
	if c == nil || c.deps.Evidence == nil {
		return nil
	}
	return c.deps.Evidence()
}

// SetSkills 装配子代理 skill 目录 actor（skill.Registry 自带锁；传 nil 关闭
// skill 块，降级）。
func (c *Coordinator) SetSkills(registry *skill.Registry) {
	if c == nil {
		return
	}
	c.skills = registry
}

// Budget 返回节点子代理的迭代轮数预算：优先节点输入参数（plan_load 节点
// budget 字段，plan.md §7.3），缺省回退 seele.yaml limits 的
// plan_node_max_loops（默认 15）。上限由 dto.PlanPolicy 校验。
func (c *Coordinator) Budget(input plan.SeelexNodeInput) NodeBudgetInfo {
	var limits config.AccountLimits
	if c != nil && c.deps.AccountLimits != nil {
		limits = c.deps.AccountLimits()
	}
	var maxLoops int
	if c != nil {
		maxLoops = c.deps.Limits.PlanNodeMaxLoops
	}
	budget := NodeBudgetInfo{MaxLoops: maxLoops, MaxOutputTokens: limits.MaxOutputTokens}
	if input.Budget != nil {
		if input.Budget.MaxLoops > 0 {
			budget.MaxLoops = input.Budget.MaxLoops
		}
		if input.Budget.MaxOutputTokens > 0 {
			budget.MaxOutputTokens = input.Budget.MaxOutputTokens
		}
	}
	return budget
}

// GoalSkillActive 读取 Runtime 持有的不可变可见性投影。
func (c *Coordinator) GoalSkillActive() bool {
	if c == nil || c.deps.GoalSkillActive == nil {
		return false
	}
	return c.deps.GoalSkillActive()
}

// PromptBlocks 构建节点级 PromptBlock：子代理章程（Claude Code 风格结构化
// 提示词：Role/Context/Task/Investigation/Constraints/Verification）。
// 父证据、预算、收尾约束全部并入章程（单一权威契约，不再拆碎块）。
func (c *Coordinator) PromptBlocks(input plan.SeelexNodeInput) []seelectx.PromptBlock {
	blocks := make([]seelectx.PromptBlock, 0, 3)
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-charter",
		Messages: []types.Message{{
			Role:    "user",
			Content: stringPtr(NodeSubagentCharter(input, c.Budget(input), c.ParentEvidence())),
		}},
	})
	blocks = append(blocks, c.skillBlocks(input)...)
	return blocks
}

// Assembler 返回节点子代理会话的 RequestAssembler：把 AgentNode 注入 ctx 的
// 节点级 PromptBlocks 合并进每次请求（保序：静态块在前，节点块在后，再跟
// working history），并继承主代理的稳定上下文块（project/stack/memory；
// 缓存前缀友好），委托默认装配器完成拼接。
func (c *Coordinator) Assembler() seelectx.RequestAssembler {
	return ScopeAssembler{Coordinator: c}
}

// skillBlocks 构建子代理 skill 块：
//   - node-skill-catalog：全部可见 skill 的名称 + 描述（目录，模型可感知
//     可用技能；与主代理"读取 skill 目录"对齐）；
//   - node-skill-active：与节点目标匹配的 skill 完整指令（名称分词/描述词
//     出现在节点 input 中即激活；未匹配 → 不注入，目录块仍在）。
func (c *Coordinator) skillBlocks(input plan.SeelexNodeInput) []seelectx.PromptBlock {
	if c == nil || c.skills == nil {
		return nil
	}
	skills := c.skills.All()
	if len(skills) == 0 {
		return nil
	}
	blocks := make([]seelectx.PromptBlock, 0, 2)
	var catalog strings.Builder
	catalog.WriteString("## 可用技能 (Skill Catalog)\n")
	for _, s := range skills {
		catalog.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
	}
	blocks = append(blocks, seelectx.PromptBlock{
		Name:     "node-skill-catalog",
		Messages: []types.Message{{Role: "user", Content: stringPtr(catalog.String())}},
	})
	if matched := MatchNodeSkills(input.Input, skills); len(matched) > 0 {
		var active strings.Builder
		active.WriteString("## 激活技能 (Active Skills)\n")
		for _, s := range matched {
			active.WriteString(fmt.Sprintf("### %s\n%s\n", s.Name, s.Prompt))
		}
		blocks = append(blocks, seelectx.PromptBlock{
			Name:     "node-skill-active",
			Messages: []types.Message{{Role: "user", Content: stringPtr(active.String())}},
		})
	}
	return blocks
}

// MarkStarted 节点首次组装请求（真正开始执行，SSE 流开启）→ queued 转
// running（B5 状态准确性：会话挂载不等于执行，running 必须是在工作）。
func (c *Coordinator) MarkStarted(nodeID string) {
	if c == nil || c.deps.Tree == nil || c.deps.Tasks == nil {
		return
	}
	c.startedMu.Lock()
	if _, started := c.started[nodeID]; started {
		c.startedMu.Unlock()
		return
	}
	c.started[nodeID] = struct{}{}
	c.startedMu.Unlock()
	c.deps.Tree.MarkRunning(nodeID)
	_, _ = c.deps.Tasks.SetStatus("subagent:"+nodeID, dto.TaskRunning, "stream started")
}

// inheritedBlocks 返回子代理会话继承的主代理稳定上下文块：
// project（项目模块语义）→ stack（now using 栈顶）。这些块内容与主代理
// 装配器同源（同一 provider），会话内稳定，插在节点块之前构成可缓存前缀。
// 动态 memory 块由装配器单独放最后，不在此列。
func (c *Coordinator) inheritedBlocks() []seelectx.PromptBlock {
	if c == nil || c.deps.InheritedBlocks == nil {
		return nil
	}
	return c.deps.InheritedBlocks()
}

// ScopeAssembler 是 Coordinator 的请求装配器实现（见 Coordinator.Assembler）。
// Coordinator 为 nil 时退化为默认装配行为（继承块/记忆块/markStarted 均跳过）。
type ScopeAssembler struct {
	Coordinator *Coordinator
}

// Assemble 实现 seelectx.RequestAssembler。
func (a ScopeAssembler) Assemble(ctx context.Context, request seelectx.AssemblyRequest) (seelectx.AssembledRequest, error) {
	if a.Coordinator != nil {
		if scope, ok := model.NodeScopeFromContext(ctx); ok && scope.NodeID != "" && scope.Role == model.RoleSubAgent {
			a.Coordinator.MarkStarted(scope.NodeID)
		}
	}
	nodeBlocks := NodePromptBlocksFromContext(ctx)
	// 装配顺序（前缀稳定优先）：system → 继承块（project/stack）→ 会话其余
	// 块 → 节点块（charter/skills）→ memory → working history。
	systemBlocks, rest := splitSystemBlocks(request.Blocks)
	merged := make([]seelectx.PromptBlock, 0, len(request.Blocks)+len(nodeBlocks)+3)
	merged = append(merged, systemBlocks...)
	if a.Coordinator != nil {
		merged = append(merged, a.Coordinator.inheritedBlocks()...)
	}
	merged = append(merged, rest...)
	merged = append(merged, nodeBlocks...)
	if a.Coordinator != nil && a.Coordinator.deps.RelatedMemory != nil {
		if memory := a.Coordinator.deps.RelatedMemory(ctx, seelexctx.LastUserQuery(request.WorkingHistory)); len(memory) > 0 {
			merged = append(merged, memory...)
		}
	}
	request.Blocks = merged
	return seelectx.DefaultRequestAssembler{}.Assemble(ctx, request)
}

// splitSystemBlocks 把 system 块拆到最前（provider/缓存前缀约定），其余
// 保持原序；无 system 块时两部分均为空/原序。
func splitSystemBlocks(blocks []seelectx.PromptBlock) (systemBlocks, rest []seelectx.PromptBlock) {
	for _, block := range blocks {
		if block.Name == "system" {
			systemBlocks = append(systemBlocks, block)
		} else {
			rest = append(rest, block)
		}
	}
	return systemBlocks, rest
}

// stringPtr 返回字符串指针（PromptBlock 内容载体）。
func stringPtr(value string) *string { return &value }
