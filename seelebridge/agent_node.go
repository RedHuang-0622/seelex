package seelebridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/skill"
)

// ──── 节点域 Runtime 门面 ─────────────────────────────────────────────
// 子代理执行域（AgentNode/预算/charter/skill 匹配）已迁入 seelebridge/node，
// 本文件只保留 Runtime 侧委托方法与节点会话装配器。

// registerNodeSession 注册运行中的子代理会话（详情查询看数据面）。
// 子代理会话是独立 actor（自己的锁），运行中读取安全——与主会话无关。
func (r *Runtime) registerNodeSession(nodeID string, sess *frameworkSession.Session, goal string) {
	r.subagentSessions.Register(nodeID, sess, goal)
	r.subagentTree.NoteSession(nodeID, sess)
}

// completeSubagentNode 写入 fork 子代理树的节点终态（子代理树可视化；
// 幂等：非 fork 节点 no-op）。summary 是节点最终输出（会话摘要）。
func (r *Runtime) completeSubagentNode(nodeID, summary string, err error) {
	if r == nil || r.subagentTree == nil {
		return
	}
	r.subagentTree.CompleteSubagentNode(nodeID, summary, err)
	// task 注册表同步终态（B5：生命周期即打点源；application 侧同步因状态
	// 一致而跳过，不重复打点）。非 fork 节点注册表无记录 → 忽略。
	if r.tasks != nil {
		status := dto.TaskCompleted
		if err != nil {
			status = dto.TaskFailed
		}
		_, _ = r.tasks.SetStatus("subagent:"+nodeID, status, summary)
	}
}

// unregisterNodeSession 结束注册并保留最后快照（节点结束后详情仍可看）。
func (r *Runtime) unregisterNodeSession(nodeID string) {
	if snap := r.subagentSessions.Unregister(nodeID); snap != nil {
		r.subagentTree.NoteSnapshot(nodeID, snap)
	}
}

// NodeSessionConversation 返回节点子代理的会话记录：运行中 → 子会话
// History（实时）；已结束 → 最后快照。只读子代理 actor，绝不触碰主会话。
func (r *Runtime) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	if r == nil || r.subagentSessions == nil {
		return nil, false
	}
	return r.subagentSessions.Conversation(nodeID)
}

// NodeContextSnapshot 返回节点子代理的结构化上下文快照（详情弹窗
// "上下文"标签数据面）：运行中实时导出（Goal/Findings/Decisions/
// TokenEstimate，同 mergeBack 导出面）；已结束返回结束时刻快照。
func (r *Runtime) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if r == nil || r.subagentSessions == nil {
		return nil, false
	}
	return r.subagentSessions.ContextSnapshot(nodeID)
}

func (r *Runtime) appendNodePhase(ctx context.Context, nodeID, status string) {
	if r == nil || r.planExecutor == nil {
		return
	}
	r.planExecutor.AppendPhase(ctx, nodeID, status)
}

// nodeParentEvidence 返回 Runtime 持有的父证据快照（子代理可读面）。
func (r *Runtime) nodeParentEvidence() *snapshot.ContextSnapshot {
	if r == nil || r.subagentContext == nil {
		return nil
	}
	return r.subagentContext.NodeParentEvidence()
}

// ──── 节点会话装配器（组合职责，暂留根包）──────────────────────────────

// nodeScopeAssembler 是节点子代理会话的 RequestAssembler：把 AgentNode
// 注入 ctx 的节点级 PromptBlocks 合并进每次请求（保序：静态块在前，节点
// 块在后，再跟 working history），并继承主代理的稳定上下文块
// （project/stack/memory；缓存前缀友好），委托默认装配器完成拼接。
// 首次组装 = 节点真正开始执行（SSE 流开启）→ 通知 runtime 把 queued 置 running。
type nodeScopeAssembler struct {
	runtime *Runtime
}

func (assembler nodeScopeAssembler) Assemble(ctx context.Context, request seelectx.AssemblyRequest) (seelectx.AssembledRequest, error) {
	if assembler.runtime != nil {
		if scope, ok := NodeScopeFromContext(ctx); ok && scope.NodeID != "" && scope.Role == model.RoleSubAgent {
			assembler.runtime.markNodeStarted(scope.NodeID)
		}
	}
	nodeBlocks := nodePromptBlocksFromContext(ctx)
	// 装配顺序（前缀稳定优先）：system → 继承块（project/stack）→ 会话其余
	// 块 → 节点块（charter/skills）→ memory → working history。
	systemBlocks, rest := splitSystemBlocks(request.Blocks)
	merged := make([]seelectx.PromptBlock, 0, len(request.Blocks)+len(nodeBlocks)+3)
	merged = append(merged, systemBlocks...)
	if assembler.runtime != nil {
		merged = append(merged, assembler.runtime.inheritedSubagentBlocks()...)
	}
	merged = append(merged, rest...)
	merged = append(merged, nodeBlocks...)
	if assembler.runtime != nil && assembler.runtime.sessionContextStore() != nil {
		if memory := assembler.runtime.relatedMemoryBlocks(ctx, seelexctx.LastUserQuery(request.WorkingHistory)); len(memory) > 0 {
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

// inheritedSubagentBlocks 返回子代理会话继承的主代理稳定上下文块：
// project（项目模块语义）→ stack（now using 栈顶）。这些块内容与主代理
// 装配器同源（同一 provider），会话内稳定，插在节点块之前构成可缓存前缀。
// 动态 memory 块由装配器单独放最后，不在此列。
func (r *Runtime) inheritedSubagentBlocks() []seelectx.PromptBlock {
	if r == nil {
		return nil
	}
	blocks := make([]seelectx.PromptBlock, 0, 2)
	if project := r.projectBlock(); project != nil {
		blocks = append(blocks, *project)
	}
	blocks = append(blocks, r.stackBlocks()...)
	return blocks
}

// markNodeStarted 节点首次组装请求（真正开始执行，SSE 流开启）→ queued 转
// running（B5 状态准确性：会话挂载不等于执行，running 必须是"在工作"）。
func (r *Runtime) markNodeStarted(nodeID string) {
	if r == nil || r.subagentTree == nil || r.tasks == nil {
		return
	}
	r.nodeStartedMu.Lock()
	if _, started := r.nodeStarted[nodeID]; started {
		r.nodeStartedMu.Unlock()
		return
	}
	r.nodeStarted[nodeID] = struct{}{}
	r.nodeStartedMu.Unlock()
	r.subagentTree.MarkRunning(nodeID)
	_, _ = r.tasks.SetStatus("subagent:"+nodeID, dto.TaskRunning, "stream started")
}

// nodePromptBlocks 构建节点级 PromptBlock：子代理章程（Claude Code 风格
// 结构化提示词：Role/Context/Task/Investigation/Constraints/Verification）。
// 父证据、预算、收尾契约全部并入章程（单一权威契约，不再拆碎块）。
func (r *Runtime) nodePromptBlocks(input plan.SeelexNodeInput) []seelectx.PromptBlock {
	blocks := make([]seelectx.PromptBlock, 0, 3)
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-charter",
		Messages: []types.Message{{
			Role:    "user",
			Content: stringPtr(node.NodeSubagentCharter(input, r.nodeBudget(input), r.nodeParentEvidence())),
		}},
	})
	blocks = append(blocks, r.nodeSkillBlocks(input)...)
	return blocks
}

// SetSkillRegistry 装配子代理 skill 目录 actor（skill.Registry 自带锁，
// 读写经其方法进出；装配一次性写入、运行期只读消费，与 filesystem actor
// 同构，无需外层锁）。传入 nil 关闭 skill 块（降级）。
func (r *Runtime) SetSkillRegistry(registry *skill.Registry) {
	r.skills = registry
}

// goalSkillActive reads the Runtime-owned immutable visibility projection.
func (r *Runtime) goalSkillActive() bool {
	if r == nil {
		return false
	}
	projection := r.visibilityProjection.Load()
	return projection != nil && projection.GoalSkillActive
}

// nodeSkillBlocks 构建子代理 skill 块：
//   - node-skill-catalog：全部可见 skill 的名称 + 描述（目录，模型可感知
//     可用技能；与主代理"读取 skill 目录"对齐）；
//   - node-skill-active：与节点目标匹配的 skill 完整指令（名称分词/描述词
//     出现在节点 input 中即激活；未匹配 → 不注入，目录块仍在）。
func (r *Runtime) nodeSkillBlocks(input plan.SeelexNodeInput) []seelectx.PromptBlock {
	if r.skills == nil {
		return nil
	}
	skills := r.skills.All()
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
	if matched := node.MatchNodeSkills(input.Input, skills); len(matched) > 0 {
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

// nodeBudget 返回节点子代理的迭代轮数预算：优先节点输入参数（plan_load
// 节点 budget 字段，plan.md §7.3），缺省回退 seele.yaml limits 的
// plan_node_max_loops（默认 15）。上限由 dto.PlanPolicy 校验。
func (r *Runtime) nodeBudget(input plan.SeelexNodeInput) node.NodeBudgetInfo {
	limits := r.currentAccountLimits()
	budget := node.NodeBudgetInfo{MaxLoops: r.limits.PlanNodeMaxLoops, MaxOutputTokens: limits.MaxOutputTokens}
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

func stringPtr(value string) *string { return &value }
