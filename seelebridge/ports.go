package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	frameworkmcp "github.com/RedHuang-0622/Seele/tools/mcp"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/mcp"
	"github.com/RedHuang-0622/seelex/seelebridge/scheduler"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/skill"
)

// ports.go 承载 Runtime 对 application/contract 端口的实现方法（组合根公开面）。
// 域实现已下沉子包；本文件只保留类型别名与逐行委托，DTO 统一走
// application/contract/dto。mainAgentNodeID 是子代理树的合成根节点 ID。
const mainAgentNodeID = model.MainAgentNodeID

// ── task 端口 ─────────────────────────────────────────────────────

// TaskSnapshot 返回 task 注册表只读快照（worktable 投影数据源）。
func (r *Runtime) TaskSnapshot() []dto.TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.tasks.Snapshot()
}

// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (r *Runtime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.Add(spec)
}

// ResolveTaskByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (r *Runtime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.ResolveByKey(key)
}

// TaskSetStatus 更新 task 状态（生命周期打点）。
func (r *Runtime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.SetStatus(id, status, evidence)
}

// TaskAttachParticipant 记录参与节点（并行执行证明）。
func (r *Runtime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.AttachParticipant(id, participant)
}

// TaskAppendTrace 追加任务生命周期 trace 点。
func (r *Runtime) TaskAppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.AppendTrace(id, point)
}

// SwitchSessionTasks 会话切换时整体替换注册表快照。
func (r *Runtime) SwitchSessionTasks(records []dto.TaskRecord) {
	if r == nil || r.tasks == nil {
		return
	}
	_ = r.tasks.ReplaceAll(records)
}

// TaskChangedChannel 返回 task.changed 输出 channel（CSP：变更即投递）。
func (r *Runtime) TaskChangedChannel() <-chan dto.TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.tasks.TaskChanged()
}

// RegisterTaskTerminalTools 把终态工具 provider 注册进工具注册表（幂等）。
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
	if r == nil || r.node == nil {
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
// 类型本体在 seelebridge/worktree 域。
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

// ── MCP 端口 ──────────────────────────────────────────────────────

// MCPServer is the transport-neutral MCP configuration consumed by Seelex.
type MCPServer = mcp.Server

// BreakerEvents returns a read-only channel of breaker events.
// The consumer (mcpstack.ListenBreaker) runs automatically when AttachMCP is called.
func (r *Runtime) BreakerEvents() <-chan frameworkmcp.BreakerEvent {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.BreakerEvents()
}

// AttachMCP connects and registers a new MCP server.
// Automatically initializes the breaker channel, starts the trace listener,
// and refreshes the tool list.
func (r *Runtime) AttachMCP(ctx context.Context, cfg MCPServer) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Attach(ctx, cfg)
}

// AttachMCPServer 是 plugin 域使用的展开入参版 AttachMCP。
func (r *Runtime) AttachMCPServer(
	ctx context.Context,
	name, transport, command string,
	args, env []string,
	url string,
) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.AttachServer(ctx, name, transport, command, args, env, url)
}

// RegisterLazyMCP 登记 MCP 服务器配置但不连接（冷启动：启动路径零 MCP 进程）。
func (r *Runtime) RegisterLazyMCP(name string, cfg MCPServer) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.RegisterLazy(name, cfg)
}

// LazyMCPServerNames 返回已登记但尚未连接的 MCP 服务器名（按字典序）。
func (r *Runtime) LazyMCPServerNames() []string {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.LazyNames()
}

// LoadMCP 按需连接已登记的 MCP 服务器（冷启动加载点）。
func (r *Runtime) LoadMCP(ctx context.Context, name string) (int, error) {
	if r == nil || r.mcpManager == nil {
		return 0, nil
	}
	return r.mcpManager.Load(ctx, name)
}

// DetachMCP 断开并注销 MCP 服务器。
func (r *Runtime) DetachMCP(name string) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Detach(name)
}

// RefreshMCP 刷新指定 MCP 服务器的工具列表。
func (r *Runtime) RefreshMCP(ctx context.Context, name string) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Refresh(ctx, name)
}

// MCPServerNames 返回已连接 MCP 服务器名（按字典序）。
func (r *Runtime) MCPServerNames() []string {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Names()
}

// IsMCPAlive 轻量 ping 检查 MCP 服务器是否存活（2s 超时）。
func (r *Runtime) IsMCPAlive(name string) bool {
	if r == nil || r.mcpManager == nil {
		return false
	}
	return r.mcpManager.IsAlive(name)
}

// MCPServerStatus 返回 MCP 服务器健康状态（alive + tool count + error）。
func (r *Runtime) MCPServerStatus(name string) (alive bool, tools int, err error) {
	if r == nil || r.mcpManager == nil {
		return false, 0, nil
	}
	return r.mcpManager.Status(name)
}

// ── plugin 端口 ───────────────────────────────────────────────────

// DefinePlugin 定义或替换一个插件的可见性快照。
func (r *Runtime) DefinePlugin(name, description string, include, exclude []string) error {
	if r == nil || r.plugins == nil {
		return nil
	}
	return r.plugins.Define(name, description, include, exclude)
}

// UndefinePlugin 删除插件定义；若其为当前激活插件则一并停用。
func (r *Runtime) UndefinePlugin(name string) {
	if r == nil || r.plugins == nil {
		return
	}
	r.plugins.Undefine(name)
}

// ActivatePlugin 激活插件（未定义返回显式错误）。
func (r *Runtime) ActivatePlugin(name string) error {
	if r == nil || r.plugins == nil {
		return nil
	}
	return r.plugins.Activate(name)
}

// DeactivatePlugin 停用当前插件。
func (r *Runtime) DeactivatePlugin() {
	if r == nil || r.plugins == nil {
		return
	}
	r.plugins.Deactivate()
}

// ActivePlugin 返回当前激活插件名。
func (r *Runtime) ActivePlugin() string {
	if r == nil || r.plugins == nil {
		return ""
	}
	return r.plugins.Active()
}

// ── 定时周期任务端口 ──────────────────────────────────────────────

// ScheduledTaskKind 定时/周期任务类型（DTO 别名）。
type ScheduledTaskKind = dto.ScheduledTaskKind

const (
	ScheduledTaskCommand = dto.ScheduledTaskCommand
	ScheduledTaskPrompt  = dto.ScheduledTaskPrompt
)

// ScheduledCommand 白名单命令描述（登记即信任；argv 固定直传，不解析用户文本）。
type ScheduledCommand = dto.ScheduledCommand

// ScheduledCommandInfo 白名单命令展示信息（GUI 新建弹窗下拉数据源）。
type ScheduledCommandInfo = dto.ScheduledCommandInfo

// ScheduledTaskSpec 创建任务入参（GUI Bridge 输入；RunAt 非零 = 一次性定时任务）。
type ScheduledTaskSpec = dto.ScheduledTaskSpec

// ScheduledTaskStatus 任务快照 DTO（GUI 定时任务面板消费）。
type ScheduledTaskStatus = dto.ScheduledTaskStatus

// ScheduledPromptExecutor 提示词任务执行器（main 装配注入：application
// Submit 复用当前主会话；nil = prompt 任务不可创建）。
type ScheduledPromptExecutor = scheduler.PromptExecutor

// RegisterScheduledCommand 登记白名单命令（重复键拒绝；main 装配调用）。
func (r *Runtime) RegisterScheduledCommand(command ScheduledCommand) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.RegisterCommand(command)
}

// ScheduledCommands 返回白名单命令展示信息（GUI 新建弹窗数据源）。
func (r *Runtime) ScheduledCommands() []ScheduledCommandInfo {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.CommandInfos()
}

// ScheduleTask 创建并启动一个定时/周期任务（校验入参；返回创建后的快照）。
func (r *Runtime) ScheduleTask(ctx context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	if r == nil || r.scheduler == nil {
		return nil, errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.Schedule(ctx, spec)
}

// CancelScheduledTask 取消并移除定时/周期任务。
func (r *Runtime) CancelScheduledTask(id string) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.CancelTask(id)
}

// ScheduledTasksSnapshot 返回定时/周期任务只读快照（application 快照投影数据源）。
func (r *Runtime) ScheduledTasksSnapshot() []ScheduledTaskStatus {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.Snapshot()
}

// SetScheduledPromptExecutor 注入提示词任务执行器（nil = 禁用 prompt 任务）。
func (r *Runtime) SetScheduledPromptExecutor(executor ScheduledPromptExecutor) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.SetPromptExecutor(executor)
}

// SetSchedulerObserver 注入调度器状态变化通知（main 接 application 投影发布）。
func (r *Runtime) SetSchedulerObserver(observer func()) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.SetObserver(observer)
}

// ── 历史检索端口 ──────────────────────────────────────────────────

// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录（GUI 历史检索
// 面板与 search_history 工具共享的数据面；query 非空校验由调用方/检索器
// 双层执行，limit 为 token 预算在 search 包内 clamp 到硬上限）。
func (r *Runtime) SearchHistory(ctx context.Context, query string, limit int) (search.Result, error) {
	if r == nil {
		return search.Result{}, errors.New("search_history: runtime is unavailable")
	}
	router := r.durableHistoryRouter()
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

// ── todolist/task 端口 ────────────────────────────────────────────

// TodoSnapshot 返回当前清单只读拷贝（application 快照投影数据源；主代理
// 每次 todolist_* 工具完成经 runtime.changed 增量带到 GUI）。
func (r *Runtime) TodoSnapshot() []dto.TodoItem {
	if r == nil || r.tasks == nil {
		return nil
	}
	records := r.tasks.TodoSnapshot()
	items := make([]dto.TodoItem, 0, len(records))
	for _, record := range records {
		items = append(items, task.TaskToTodoItem(record))
	}
	return items
}

// SetTodoStatus 设置指定待办项的三态（GUI 工作表格人工状态更新入口；
// 只改状态，不新增工具族——todolist 仍是 harness 默认工具族）。
func (r *Runtime) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	if r == nil || r.tasks == nil {
		return errors.New("todolist: unavailable")
	}
	if !task.ValidTodoStatus(status) {
		return fmt.Errorf("todolist: invalid status %q (want pending|doing|done)", status)
	}
	_, err := r.tasks.SetTodoStatusByIndex(index, task.TodoToTaskStatus(status))
	return err
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
