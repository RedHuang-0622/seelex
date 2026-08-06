package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/merger"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/skill"
)

// SeelexAgentNode 是 plan kind:agent 节点的子代理执行包装（plan.md §3.3.1）。
//
// 它包装 bridge.NewAgentFactory 产物（factory func() node.AgentFactory）：
// Run 时先把节点作用域（NodeScope）与节点级 PromptBlocks（目标 + 父证据 +
// 预算）注入 ctx，再委托节点自己的 Session 执行。可见性策略 / 账号选择器 /
// 节点装配器都从 ctx 读取作用域与块，因此并行的子代理各得其域，不需要
// 框架侧的分支运行时。
type SeelexAgentNode struct {
	node.BaseNode
	input   SeelexNodeInput
	factory func() node.AgentFactory      // bridge.NewAgentFactory 产物
	scope   func() NodeScope              // 惰性解析：plan_run 时 binding 已冻结
	blocks  func() []seelectx.PromptBlock // 节点级 PromptBlocks（目标/证据/预算）
	runtime *Runtime                      // merge-back 回传面（父会话/父证据/遥测）
}

// newSeelexAgentNode 构造 agent 节点包装。scope/blocks 经闭包延迟解析，
// 使 plan_load（buildNode）到 plan_run 之间冻结的 PlanBranchBinding
// 与父证据都能被观察到。
func newSeelexAgentNode(spec codec.NodeSpec[SeelexNodeInput], runtime *Runtime) *SeelexAgentNode {
	return &SeelexAgentNode{
		BaseNode: node.NewBaseNode(spec.ID, node.KindAgent),
		input:    spec.Input,
		factory:  runtime.currentAgentFactory,
		scope:    func() NodeScope { return nodeScopeFor(runtime, spec.ID) },
		blocks:   func() []seelectx.PromptBlock { return runtime.nodePromptBlocks(spec.Input) },
		runtime:  runtime,
	}
}

// registerNodeSession 注册运行中的子代理会话（详情查看数据面）。
// 子代理会话是独立 actor（自己的锁），运行中读取安全——与主会话无关。
// goal 是节点目标（节点输入），NodeContextSnapshot 导出时复用。
func (r *Runtime) registerNodeSession(nodeID string, sess *frameworkSession.Session, goal string) {
	r.nodeSessionsMu.Lock()
	r.nodeSessions[nodeID] = sess
	r.nodeGoals[nodeID] = goal
	r.nodeSessionsMu.Unlock()
}

// unregisterNodeSession 结束注册并保留最后快照（节点结束后详情仍可看）。
// 同时导出结束时的结构化上下文快照（Findings/Decisions/TokenEstimate，
// 与 mergeBack 同一导出面；运行中的实时导出见 NodeContextSnapshot）。
func (r *Runtime) unregisterNodeSession(nodeID string) {
	r.nodeSessionsMu.Lock()
	sess := r.nodeSessions[nodeID]
	delete(r.nodeSessions, nodeID)
	goal := r.nodeGoals[nodeID]
	r.nodeSessionsMu.Unlock()
	if sess == nil {
		return
	}
	history := sess.History()
	if snap := seelexctx.ExportSnapshot(sess, r.Tracer(), goal); snap != nil {
		r.nodeSessionsMu.Lock()
		r.nodeContextSnapshots[nodeID] = snap
		r.nodeSessionsMu.Unlock()
	}
	r.nodeSessionsMu.Lock()
	r.nodeSnapshots[nodeID] = history
	r.nodeSessionsMu.Unlock()
}

// NodeSessionConversation 返回节点子代理的会话记录：
// 运行中 → 子会话 History（实时）；已结束 → 最后快照。
// 只读子代理 actor，绝不触碰主会话（死锁教训，见 actor.go）。
func (r *Runtime) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	r.nodeSessionsMu.Lock()
	sess := r.nodeSessions[nodeID]
	snap, ok := r.nodeSnapshots[nodeID]
	r.nodeSessionsMu.Unlock()
	if sess != nil {
		return sess.History(), true
	}
	return snap, ok
}

// NodeContextSnapshot 返回节点子代理的结构化上下文快照（详情弹窗
// "上下文"标签数据面）：运行中实时导出（Goal/Findings/Decisions/
// TokenEstimate，同 mergeBack 导出面）；已结束返回结束时快照。
// 只读子代理 actor，安全。
func (r *Runtime) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if r == nil {
		return nil, false
	}
	r.nodeSessionsMu.Lock()
	sess := r.nodeSessions[nodeID]
	snap := r.nodeContextSnapshots[nodeID]
	goal := r.nodeGoals[nodeID]
	r.nodeSessionsMu.Unlock()
	if sess != nil {
		return seelexctx.ExportSnapshot(sess, r.Tracer(), goal), true
	}
	return snap, snap != nil
}

// parentSnapshot 返回 Runtime 自有父证据快照。
func (n *SeelexAgentNode) parentSnapshot() *snapshot.ContextSnapshot {
	if n == nil || n.runtime == nil {
		return nil
	}
	return n.runtime.nodeParentEvidence()
}

// traceSource 返回遥测追踪源（子代理 Findings/Decisions 提取）。
func (n *SeelexAgentNode) traceSource() provider.TraceSource {
	if n == nil || n.runtime == nil {
		return nil
	}
	return n.runtime.Tracer()
}

// now 是时间戳提供者（测试可覆盖；Date.now 语义保持确定）。
var now = time.Now

// Run 注入节点作用域与节点级 PromptBlocks 后委托节点 Session 执行
// （目标作为会话 system prompt，节点输入作为本轮请求）。
// worktree 生命周期（plan.md §3）：RoleSubAgent 节点先开 worktree 并把
// NodeScope.WorkspaceID 指向它（scoped_tools 按节点分根），执行结束后
// 变基兜底 + 合并审批 + merge + 清理。执行成功后把子代理会话的结构化
// 上下文合并回父会话（merge-back）。
func (n *SeelexAgentNode) Run(ctx context.Context, _ *workplanTypes.WorkflowContext) (string, error) {
	// 1) 节点身份：可见性策略 / 账号选择器 / 装配器从 ctx 读取。
	scope := n.scope()
	if scope.Role == RoleSubAgent {
		n.runtime.appendNodePhase(ctx, n.ID(), "worktree_creating")
	}
	// 2) worktree 生命周期（开）：RoleSubAgent 节点创建独立 worktree，
	//    WorkspaceID 指向 worktree 根（降级：非 git / 失败 → 共享工作区）。
	wt := n.runtime.beginNodeWorktree(scope, n.ID())
	if wt != nil {
		scope.WorkspaceID = wt.Path
	}
	ctx = WithNodeScope(ctx, scope)
	if scope.Role == RoleSubAgent {
		n.runtime.appendNodePhase(ctx, n.ID(), "running")
	}
	// 3) 节点上下文（PromptBlocks）：目标 + 父证据 + 预算 + skill + 收尾契约。
	if n.blocks != nil {
		ctx = withNodePromptBlocks(ctx, n.blocks())
	}
	factory := n.factory()
	if factory == nil {
		return "", fmt.Errorf("agent node %q: agent factory is not configured", n.ID())
	}
	agent := factory.NewAgent(n.input.Input)
	// 节点级预算（input.budget 优先，回退 limits）：动态覆盖节点会话的
	// ReAct 循环上限（session.SetMaxLoops，chat.go 动态方法）。
	if sess, ok := agent.(*frameworkSession.Session); ok {
		sess.SetMaxLoops(n.runtime.nodeBudget(n.input).MaxLoops)
		// 详情查看数据面：注册子会话（结束后快照留底；goal 供上下文导出）。
		n.runtime.registerNodeSession(n.ID(), sess, n.input.Input)
		defer n.runtime.unregisterNodeSession(n.ID())
	}
	result, err := agent.Chat(ctx, n.input.Input)
	if err == nil {
		n.mergeBack(ctx, agent, n.input.Input)
	}
	// 4) worktree 生命周期（收尾）：变基兜底 → 合并审批 → merge → 清理。
	//    成功路径清理；失败路径保留现场（节点 failed 时主代理可查）。
	if wt != nil {
		if err == nil {
			err = n.runtime.finishNodeWorktree(ctx, n.ID(), wt)
		}
		if err == nil {
			n.runtime.releaseNodeWorktree(n.ID())
		}
	}
	return result, err
}

func (r *Runtime) appendNodePhase(ctx context.Context, nodeID, status string) {
	if r == nil || r.planEvents == nil {
		return
	}
	r.planRunMu.RLock()
	runID := r.currentPlanRunID
	r.planRunMu.RUnlock()
	r.planEvents.AppendPhase(ctx, r.currentPlanBranchBinding(), runID, nodeID, status)
}

// mergeBack 把子代理会话的结构化上下文（Findings/Decisions/Constraints/
// TokenEstimate）合并回父会话：子快照 + 父快照 → merger.MergeBack →
// Format() 文本经 sink 回传（application 侧排队，下一次 ChatStream 开始前
// 注入父会话）。
//
// 重要：不得直接调用父会话的 AppendHistory/History——plan_run 作为主代理的
// 工具调用在 Session.ChatStream 内同步执行，主会话锁被全程持有，任何子代理
// goroutine 对主会话的访问都会死锁（冒烟测试实测 19 分钟死锁）。回传必须
// 写入 Runtime 自有 mailbox（消息出）。
func (n *SeelexAgentNode) mergeBack(ctx context.Context, agent node.Agent, goal string) {
	childSession, ok := agent.(*frameworkSession.Session)
	if !ok {
		return
	}
	child := seelexctx.ExportSnapshot(childSession, n.traceSource(), goal)
	parent := n.parentSnapshot()
	if parent == nil {
		parent = &snapshot.ContextSnapshot{SourceSessionID: childSession.SessionID(), ExportedAt: now()}
	}
	if err := merger.NewMerger().MergeBack(parent, child); err != nil {
		return
	}
	content := parent.Format()
	if sink := n.mergeBackSink(); sink != nil {
		sink(content)
	}
}

// mergeBackSink returns the Runtime-owned bounded mailbox writer. It never
// calls Application or the main session while a subagent is completing.
func (n *SeelexAgentNode) mergeBackSink() func(string) {
	if n == nil || n.runtime == nil {
		return nil
	}
	return n.runtime.enqueueSubagentContext
}

// nodeScopeFor 解析节点作用域：新执行模型下分支即节点（BranchID = NodeID），
// 角色按 binding 与 entry 判定（与分支账号路由语义一致）。
func nodeScopeFor(runtime *Runtime, nodeID string) NodeScope {
	binding := runtime.currentPlanBranchBinding()
	return NodeScope{
		NodeID:      nodeID,
		Role:        roleForPlanBranch(binding, nodeID),
		BranchID:    nodeID,
		WorkspaceID: binding.WorkspaceID,
	}
}

// ── 节点级 PromptBlocks ─────────────────────────────────────────────

// nodeScopeAssembler 是节点子代理会话的 RequestAssembler：把 SeelexAgentNode
// 注入 ctx 的节点级 PromptBlocks 合并进每次请求（保序：静态块在前，节点块在后，
// 再跟 working history），委托默认装配器完成拼装。
type nodeScopeAssembler struct{}

func (nodeScopeAssembler) Assemble(ctx context.Context, request seelectx.AssemblyRequest) (seelectx.AssembledRequest, error) {
	if blocks := nodePromptBlocksFromContext(ctx); len(blocks) > 0 {
		merged := make([]seelectx.PromptBlock, 0, len(request.Blocks)+len(blocks))
		merged = append(merged, request.Blocks...)
		merged = append(merged, blocks...)
		request.Blocks = merged
	}
	return seelectx.DefaultRequestAssembler{}.Assemble(ctx, request)
}

// nodePromptBlocks 构建节点级 PromptBlock：目标 + 父证据 + 预算 + 收尾契约。
// 父证据来自 Runtime 本地不可变投影（缺省无）。
func (r *Runtime) nodePromptBlocks(input SeelexNodeInput) []seelectx.PromptBlock {
	blocks := make([]seelectx.PromptBlock, 0, 4)
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-goal",
		Messages: []types.Message{{
			Role:    "user",
			Content: stringPtr("## 节点目标 (Node Goal)\n" + input.Input),
		}},
	})
	if evidence := r.nodeParentEvidence(); evidence != nil {
		blocks = append(blocks, seelectx.PromptBlock{
			Name: "parent-evidence",
			Messages: []types.Message{{
				Role:    "user",
				Content: stringPtr(evidence.Format()),
			}},
		})
	}
	budget := r.nodeBudget(input)
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-budget",
		Messages: []types.Message{{
			Role: "user",
			Content: stringPtr(fmt.Sprintf(
				"## 节点预算 (Node Budget)\n- 最大迭代轮数: %d\n- 最大输出 tokens: %d",
				budget.MaxLoops, budget.MaxOutputTokens)),
		}},
	})
	// skill 能力（docs/2026-08-03-subagent-fork-architecture/plan.md §7.2）：
	// 子代理与主代理一样读取 skill 目录——目录块（名称+描述）始终注入，
	// 与节点目标匹配的 skill 注入完整指令（未装配 provider → 无块，降级）。
	blocks = append(blocks, r.nodeSkillBlocks(input)...)
	// 收尾契约（docs/2026-08-03-subagent-fork-architecture/plan.md §7.5）：
	// 子代理必须明确任务结束流程——commit、变基、禁止 merge、结构化 findings。
	blocks = append(blocks, seelectx.PromptBlock{
		Name: "node-finish-protocol",
		Messages: []types.Message{{
			Role: "user",
			Content: stringPtr(fmt.Sprintf(
				"## 任务结束流程 (Finish Protocol)\n"+
					"1. 完成标准：任务可验证（检查项/测试通过）才算完成。\n"+
					"2. 收尾序列（按序执行）：\n"+
					"   a. 若有文件改动：git add -A && git commit -m \"seelex/%s: <摘要>\"\n"+
					"   b. 变基：git rebase <主分支>（合并最新变更，冲突自行解决）\n"+
					"3. 明确禁止：不 merge、不 checkout 主分支、不触碰主工作区（合并是框架的事）。\n"+
					"4. 最终回复：给出结构化 findings（结论/改动文件/验证结果），供 merge-back。",
				input.ID)),
		}},
	})
	return blocks
}

// nodeBudgetInfo 是节点子代理的执行预算（渲染为 PromptBlock，并作为
// 节点 Session 的 SessionConfig 生效）。
type nodeBudgetInfo struct {
	MaxLoops        int
	MaxOutputTokens int
}

// SetSkillRegistry 装配子代理 skill 目录 actor（skill.Registry 自带锁，
// 读写经其方法进出；见 skill/skill.go）。装配一次性写入、运行期只读，
// 与 filesystem actor 同构，无需外层锁。传入 nil 关闭 skill 块（降级）。
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
//   - node-skill-active：与节点目标匹配的 skill 完整指令（名称分词/描述
//     词出现在节点 input 中即激活；未匹配 → 不注入，目录块仍在）。
//
// registry 未装配（nil）→ 无块，行为降级为当前实现。
func (r *Runtime) nodeSkillBlocks(input SeelexNodeInput) []seelectx.PromptBlock {
	if r.skills == nil {
		return nil
	}
	skills := r.skills.All() // actor 消息：Registry 内部锁，见 skill/skill.go
	if len(skills) == 0 {
		return nil
	}
	blocks := make([]seelectx.PromptBlock, 0, 2)
	// 目录块：每 skill 一行（名称 —— 描述）。
	var catalog strings.Builder
	catalog.WriteString("## 可用技能 (Skill Catalog)\n")
	for _, s := range skills {
		catalog.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
	}
	blocks = append(blocks, seelectx.PromptBlock{
		Name:     "node-skill-catalog",
		Messages: []types.Message{{Role: "user", Content: stringPtr(catalog.String())}},
	})
	// 激活块：与节点目标匹配的 skill 完整指令。
	if matched := matchNodeSkills(input.Input, skills); len(matched) > 0 {
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

// matchNodeSkills 按节点目标匹配 skill：skill 名称（含分词：code-impl →
// code/impl）或描述中的英文词出现在目标文本中即匹配。
func matchNodeSkills(input string, skills []skill.Skill) []skill.Skill {
	haystack := strings.ToLower(input)
	matched := make([]skill.Skill, 0, len(skills))
	for _, s := range skills {
		needle := strings.ToLower(s.Name)
		if strings.Contains(haystack, needle) {
			matched = append(matched, s)
			continue
		}
		for _, part := range strings.FieldsFunc(needle, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if len(part) >= 3 && strings.Contains(haystack, part) {
				matched = append(matched, s)
				goto next
			}
		}
		for _, word := range strings.Fields(s.Description) {
			w := strings.Trim(strings.ToLower(word), "，。！？、,.!?:;()（）\"'")
			if len(w) >= 3 && strings.Contains(haystack, w) {
				matched = append(matched, s)
				break
			}
		}
	next:
	}
	return matched
}

// nodeBudget 返回节点子代理的迭代轮数预算：优先节点输入参数
// （plan_load 节点 budget 字段，plan.md §7.3），缺省回退 seele.yaml
// limits 的 plan_node_max_loops（默认 15）。上限由 PlanPolicy 校验。
func (r *Runtime) nodeBudget(input SeelexNodeInput) nodeBudgetInfo {
	limits := r.currentAccountLimits()
	budget := nodeBudgetInfo{MaxLoops: r.limits.PlanNodeMaxLoops, MaxOutputTokens: limits.MaxOutputTokens}
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

// nodeParentEvidence returns a copy of Runtime's cached parent evidence. It
// never crosses back into Application or the main session while ChatStream is
// holding the framework session lock.
func (r *Runtime) nodeParentEvidence() *snapshot.ContextSnapshot {
	if r == nil {
		return nil
	}
	return cloneContextSnapshot(r.parentEvidence.Load())
}

func stringPtr(value string) *string { return &value }
