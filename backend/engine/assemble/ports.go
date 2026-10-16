package assemble

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/snapshot"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	"github.com/RedHuang-0622/seelex/backend/engine/task"
	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/backend/tool/task"
	"github.com/RedHuang-0622/seelex/backend/tool/worktree"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// ports.go 承载 Runtime 对 contract 端口的实现方法（组合根公开面）。
// 域实现已下沉子包；本文件只保留类型别名与逐行委托，DTO 统一走
// contract/dto。mainAgentNodeID 是子代理树的合成根节点 ID。
const mainAgentNodeID = model.MainAgentNodeID

// ── task 台账域端口（薄转发）────────────────────────────────────────
//
// 判据与状态都在 backend/engine/task.Ledger：跨会话台账合并（Snapshot）、
// 会话 scope 分区读写、会话级批次、清单面。本层只把 Runtime 的公开面接到域
// 实现上——nil 接收者与"注册表/清单未装配"的判据由域内**一处**给出，这里不
// 写第二份。

// ledger 返回台账域句柄：nil 接收者与"未装配"都返回 nil 句柄，判据由域自己给
// （见 Ledger 的 nil 防护）。
func (r *Runtime) ledger() *task_context.Ledger {
	if r == nil {
		return nil
	}
	return r.taskLedger
}

// TaskSnapshot 返回**项目/全局** task 表只读快照（worktable 投影数据源）。
func (r *Runtime) TaskSnapshot() []dto.TaskRecord {
	return r.ledger().Snapshot()
}

// TaskSnapshotFor 返回指定会话的 task 注册表快照（会话级读面：持久化落盘
// 与请求尾部打点块用）。
func (r *Runtime) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	return r.ledger().SnapshotFor(sessionID)
}

// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (r *Runtime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return r.ledger().Add(spec)
}

// TaskAddFor 按归属会话登记 task：当前任务会话写实时注册表，后台会话写自身
// scope 分区（R6/P2 收口：写自有域，后台任务不再污染当前注册表）。
func (r *Runtime) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return r.ledger().AddFor(sessionID, spec)
}

// ResolveTaskByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (r *Runtime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	return r.ledger().ResolveByKey(key)
}

// ResolveTaskByKeyFor 按归属会话查 task。
func (r *Runtime) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error) {
	return r.ledger().ResolveByKeyFor(sessionID, key)
}

// TaskSetStatus 更新 task 状态（生命周期打点）。
func (r *Runtime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return r.ledger().SetStatus(id, status, evidence)
}

// TaskSetStatusFor 按归属会话更新 task 状态。
func (r *Runtime) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return r.ledger().SetStatusFor(sessionID, id, status, evidence)
}

// TaskAttachParticipant 记录参与节点（并行执行证明）。
func (r *Runtime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error) {
	return r.ledger().AttachParticipant(id, participant)
}

// TaskAppendTrace 追加任务生命周期 trace 点。
func (r *Runtime) TaskAppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error) {
	return r.ledger().AppendTrace(id, point)
}

// SwitchSessionTasks 会话切换：保存旧会话注册表快照到其 scope 分区，装载
// 目标会话分区记录到当前注册表（空会话 ID = 进入草稿）。工作表格走全局读面，
// 切走/新建都不丢行。
func (r *Runtime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	r.ledger().SwitchSession(sessionID, records)
}

// TaskChangedChannel 返回 task.changed 输出 channel（CSP：变更即投递）。
func (r *Runtime) TaskChangedChannel() <-chan dto.TaskRecord {
	return r.ledger().Changed()
}

// SetCurrentTaskBatch 设置会话级默认批次（application startChat 按会话调用；
// 后台会话不覆盖活跃注册表默认批次——L3 写隔离）。
func (r *Runtime) SetCurrentTaskBatch(sessionID, batchID string) {
	r.ledger().SetBatch(sessionID, batchID)
}

// CurrentTaskBatchFor 返回指定会话的默认批次（TaskAdd 盖章辅助；空 = 未启动）。
func (r *Runtime) CurrentTaskBatchFor(sessionID string) string {
	return r.ledger().BatchFor(sessionID)
}

// TodoSnapshot 返回当前清单只读拷贝（application 快照投影数据源；主代理
// 每次 todolist_* 工具完成经 runtime.changed 增量带到 GUI）。
func (r *Runtime) TodoSnapshot() []dto.TodoItem {
	return r.ledger().TodoSnapshot()
}

// SetTodoStatus 设置指定待办项的三态（GUI 工作表格人工状态更新入口；
// 只改状态，不新增工具族——todolist 仍是 harness 默认工具族）。
func (r *Runtime) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	return r.ledger().SetTodoStatus(index, status)
}

// TodoSnapshotFor 按归属会话读清单（工具族按调用会话取数）。
func (r *Runtime) TodoSnapshotFor(sessionID string) []dto.TodoItem {
	return r.ledger().TodoSnapshotFor(sessionID)
}

// ReplaceTodoFor 按归属会话整体替换清单（todo_init）。
func (r *Runtime) ReplaceTodoFor(sessionID string, items []dto.TodoItem) error {
	return r.ledger().ReplaceTodoFor(sessionID, items)
}

// AppendTodoFor 按归属会话追加清单项（todo_add；上限与注册表同一口径）。
func (r *Runtime) AppendTodoFor(sessionID string, item dto.TodoItem, limit int) error {
	return r.ledger().AppendTodoFor(sessionID, item, limit)
}

// SetTodoStatusFor 按归属会话设置清单项三态（todo_done）。
func (r *Runtime) SetTodoStatusFor(sessionID string, index int, status task.TaskStatus) (dto.TaskRecord, error) {
	return r.ledger().SetTodoStatusFor(sessionID, index, status)
}

// SetSessionWorkspace 记录会话绑定的 workspace ID（application 在恢复/
// 物化/绑定工作区时通知；framework DurableHistory 按显式键落盘，R3 键
// 漂移收敛）。会话绑定事实，不属 task 台账域。
func (r *Runtime) SetSessionWorkspace(sessionID, workspaceID string) {
	if r == nil {
		return
	}
	r.sessionWorkspacesMu.Lock()
	r.sessionWorkspaces[sessionID] = workspaceID
	r.sessionWorkspacesMu.Unlock()
}

// RegisterTaskTerminalTools 把终态工具 provider 注册进工具注册表（幂等）。
// 状态是**工具注册表**（不属 task 台账域），故留在装配层。
func (r *Runtime) RegisterTaskTerminalTools(handler task.TaskTerminalHandler) {
	state := r.registry
	if state == nil || state.Registry == nil {
		return
	}
	_ = state.Registry.Unregister("seelex-task-terminal")
	if err := state.Registry.Register(task.NewTaskTerminalProvider(handler)); err != nil {
		return
	}
}

// ── plan 端口 ─────────────────────────────────────────────────────

// PrepareReplan atomically replaces a failed WorkPlan with a recovery plan.
// It only plans; it never invokes plan_run or retries side effects.
func (r *Runtime) PrepareReplan(ctx context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error) {
	if r == nil || r.planExecutor == nil {
		return dto.PlanPreflight{}, fmt.Errorf("plan replan: plan executor is unavailable")
	}
	return r.planExecutor.PrepareReplan(ctx, request)
}

// ── 子代理树端口 ──────────────────────────────────────────────────

// ClearSubagentTree 清空子代理树（GUI"清空"按钮入口）。
func (r *Runtime) ClearSubagentTree() error {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Clear()
}

// SubagentTreeEvents 返回子代理树生命周期信号 channel（CSP 消费者）。
func (r *Runtime) SubagentTreeEvents() <-chan struct{} {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Events()
}

// SubAgentTree 返回子代理树的只读投影（根 = 主代理）。
func (r *Runtime) SubAgentTree() []dto.SubAgentTreeNode {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Projection()
}

// SetSubagentToolCallback 注入子代理工具活动观察者（委托 session.ToolEventState）。
func (r *Runtime) SetSubagentToolCallback(callback func(subagentsession.SubagentToolEvent)) {
	if r == nil || r.toolEvents == nil {
		return
	}
	r.toolEvents.SetCallback(callback)
}

// ── 节点端口 ──────────────────────────────────────────────────────

// NodeSessionConversation 返回节点子代理的会话记录：运行中 → 子会话
// History（实时）；已结束 → 最后快照。只读子代理 actor，绝不触碰主会话。
func (r *Runtime) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	if r == nil || r.node == nil {
		return nil, false
	}
	return r.node.Conversation(nodeID)
}

// NodeContextSnapshot 返回节点子代理的结构化上下文快照（详情弹窗"上下文"标签）。
func (r *Runtime) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if r == nil || r.node == nil {
		return nil, false
	}
	return r.node.ContextSnapshot(nodeID)
}

// SetSkillRegistry 装配子代理 skill 目录 actor（skill.Registry 自带锁；
// 传 nil 关闭 skill 块，降级）。
func (r *Runtime) SetSkillRegistry(registry *skill.Registry) {
	if r == nil {
		return
	}
	// 两个读面同一份事实：子代理的 skill 块（node.SetSkills）与员工回合的技能
	// 目录（按会话装配的集合，见 runtime_role_plugins.go）。先存再判 node：
	// 没装配 node 时目录读面照样生效（角色回合不经过 node）。
	r.skills.Store(registry)
	if r.node == nil {
		return
	}
	r.node.SetSkills(registry)
}

// NodeToolResult 读回节点子代理的工具结果原始内容（ref 必须带
// node:<nodeID>: 前缀）。只读节点归档器，安全。
func (r *Runtime) NodeToolResult(nodeID, ref string) (string, bool) {
	if r == nil || r.node == nil {
		return "", false
	}
	return r.node.ToolResult(nodeID, ref)
}

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）；
// 类型本体在 backend/tool/worktree 域。
type NodeWorktreeInfo = worktree.NodeWorktreeInfo

// NodeWorktreeInfoFor 返回节点 worktree 现场信息（无现场 → false）。
func (r *Runtime) NodeWorktreeInfoFor(nodeID string) (NodeWorktreeInfo, bool) {
	if r == nil || r.worktreeMgr == nil || nodeID == "" {
		return NodeWorktreeInfo{}, false
	}
	return r.worktreeMgr.Info(nodeID)
}

// NodeStageLogs 返回 node 第一视角分阶段上下文日志（同一 subagent 会话的
// 认证面：全部阶段日志共享 SessionID）。
func (r *Runtime) NodeStageLogs(nodeID string) []model.NodeStageLog {
	if r == nil || r.node == nil || nodeID == "" {
		return nil
	}
	return r.node.StageLogs(nodeID)
}

// NodeStageEvents 返回第一视角阶段日志的实时推送通道（即时输出面）：
// 每个阶段（spawn/turn/tool/result）被记录后立即投递，消费方按 NodeID 过滤。
func (r *Runtime) NodeStageEvents() <-chan model.NodeStageLog {
	if r == nil || r.node == nil {
		return nil
	}
	return r.node.StageEvents()
}

// NodeFirstPersonView 返回 node 第一视角完整载荷：查看时间（ProbedAt）+
// 逐步产出的分阶段日志 + 语义结果。日志按记录序单调递增且早于 ProbedAt。
func (r *Runtime) NodeFirstPersonView(nodeID string) *model.NodeFirstPersonView {
	if r == nil || r.node == nil || nodeID == "" {
		return nil
	}
	return &model.NodeFirstPersonView{
		NodeID:   nodeID,
		ProbedAt: time.Now(),
		Stages:   r.node.StageLogs(nodeID),
		Result:   r.node.SemanticResult(nodeID),
	}
}

// NodeSemanticResult 返回 node 的预定义语义结果（只读；对象结构由 seelex
// 制定，非 subagent 自拟）。
func (r *Runtime) NodeSemanticResult(nodeID string) *model.NodeSemanticResult {
	if r == nil || r.node == nil || nodeID == "" {
		return nil
	}
	return r.node.SemanticResult(nodeID)
}

// DrainSubagentSemanticResults 取空子代理语义结果队列（消息队列消费面：
// mainagent / plan 下游 node 读取）。
func (r *Runtime) DrainSubagentSemanticResults() []*model.NodeSemanticResult {
	if r == nil || r.node == nil {
		return nil
	}
	return r.node.DrainSemanticResults()
}

// ── 历史检索端口 ──────────────────────────────────────────────────

// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录（GUI 历史检索
// 面板与 search_history 工具共享的数据面；query 非空校验由调用方/检索器
// 双层执行，limit 为 token 预算在 search 包内 clamp 到硬上限）。
func (r *Runtime) SearchHistory(ctx context.Context, query string, limit int) (search.Result, error) {
	if r == nil {
		return search.Result{}, errors.New("search_history: runtime is unavailable")
	}
	router := r.HistoryRouter()
	if router == nil {
		return search.Result{}, errors.New("search_history: 事件库未装配（会话持久化未启用）")
	}
	var stack search.StackSource
	if store := r.sessionContextStore(); store != nil {
		stack = store
	}
	searcher := search.New(stack, search.NewRouterEventSource(router, router.Workspace(), r.MainSessionID()))
	return searcher.Search(ctx, query, search.Options{Limit: limit})
}

// ── actor 消息边界（应用 → Runtime 单向发布）──────────────────────

// RuntimeVisibilityProjection is an immutable application-to-runtime message.
type RuntimeVisibilityProjection = dto.RuntimeVisibilityProjection

// ParentEvidenceProjection is the minimal application-owned data needed for a
// Runtime-local parent evidence snapshot. Runtime adds its own telemetry and
// stores the resulting immutable snapshot for subagents to read.
type ParentEvidenceProjection = model.ParentEvidenceProjection

// SetRuntimeVisibilityProjection publishes a value copy from Application. It
// has no synchronous reverse callback and is safe from Runtime tool hooks.
func (r *Runtime) SetRuntimeVisibilityProjection(projection RuntimeVisibilityProjection) {
	if r == nil {
		return
	}
	copy := projection
	r.visibilityProjection.Store(&copy)
}

// SetParentEvidenceProjection turns the application projection into a
// Runtime-owned immutable snapshot. A blank session clears stale evidence.
func (r *Runtime) SetParentEvidenceProjection(projection ParentEvidenceProjection) {
	if r == nil {
		return
	}
	r.subagentContext.SetParentEvidenceProjection(projection)
}
