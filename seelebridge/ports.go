package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

// TaskSnapshot 返回**项目/全局** task 表只读快照（worktable 投影数据源）：
// 实时注册表（当前会话）与各会话 scope 分区合并，按跨会话身份去重（幂等键
// 优先，否则行 ID）、稳定排序。自动条目的行 ID（`task:<n>`/`todo:<n>`）由
// 进程级分配器给出，进程内唯一（见 task.NextAutoID）——同 ID 在台账里是同一
// 行，重号会丢行。
//
// 工作表格是一条**跨会话台账**——会话只是条目的产生地，不是表格的作用域。
// 切走/新建会话都不应让先前会话的条目从表里消失（用户看的是"这个项目在办
// 什么"，不是"这个会话在办什么"）。会话级读面（持久化落盘、请求尾部打点
// 块）走 TaskSnapshotFor，仍按会话取数。
//
// 每条记录带**归属会话**（SessionID：实时注册表 = 当前会话，分区 = 分区键）：
// 台账默认全量展示，前端会话筛选轴（"仅本会话"）据此过滤——全局口径不牺牲
// "这行是谁在办的"的可判定性。
func (r *Runtime) TaskSnapshot() []dto.TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.taskSnapshotAll()
}

// taskSnapshotAll 合并实时注册表与所有会话分区（跨会话身份去重：有幂等键
// 按 Key、否则按 ID；注册表记录优先）。每条记录标注**归属会话**：实时注册表
// 的记录归当前会话，分区记录归分区键——工作表格的会话筛选轴（默认全部、可
// 切「仅本会话」）按此判定，否则合并后无法回答「这行是谁在办的」。
func (r *Runtime) taskSnapshotAll() []dto.TaskRecord {
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	live := r.tasks.Snapshot()
	records := make([]dto.TaskRecord, 0, len(live))
	seen := make(map[string]struct{}, len(live))
	for _, record := range live {
		record.SessionID = r.currentTaskSessionID
		seen[taskLedgerIdentity(record)] = struct{}{}
		records = append(records, record)
	}
	for sessionID, partition := range r.sessionTaskSnapshots {
		for _, record := range partition {
			identity := taskLedgerIdentity(record)
			if _, exists := seen[identity]; exists {
				continue
			}
			seen[identity] = struct{}{}
			record.SessionID = sessionID
			records = append(records, record)
		}
	}
	sort.SliceStable(records, func(left, right int) bool { return records[left].ID < records[right].ID })
	return records
}

// taskLedgerIdentity 是跨会话台账的去重身份：幂等键优先（注册表按 Key 去重，
// plan/subagent/todo 与 task_add 都有稳定 Key），缺失时才回退行 ID。优先级
// 不能反过来——同一 plan 节点/子代理/待办文本会在会话切换、恢复、跨进程冷读
// 时以不同来源出现，Key 才是它们的同一性；而回退行 ID 的自动号现在由进程级
// 分配器保证唯一（见 task.NextAutoID），不会把两个会话的条目并成一行、也不
// 会重号丢行（外部分配/跨版本数据仍按 ObserveAutoID 抬高水位）。
func taskLedgerIdentity(record dto.TaskRecord) string {
	if record.Key != "" {
		return "key:" + record.Key
	}
	return "id:" + record.ID
}

// TaskSnapshotFor 返回指定会话的 task 注册表快照（会话级读面：持久化落盘
// 与请求尾部打点块用）。后台会话（非当前 task 会话）返回切换时保存的分片
// 快照；当前会话返回注册表实时快照；空会话 ID 视为当前。
//
// 注意：工作表格本体取全局读面（TaskSnapshot），不是这里——本方法刻意保持
// 会话粒度，避免把别的会话的活动任务注入本会话上下文；也刻意**不**标注
// SessionID（调用方本来就知道是哪个会话，且本方法服务于落盘——归属会话由
// 容器 `SessionRecord` 表达，不必写进每条记录）。
func (r *Runtime) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == r.currentTaskSessionID {
		return r.tasks.Snapshot()
	}
	return append([]dto.TaskRecord(nil), r.sessionTaskSnapshots[sessionID]...)
}

// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (r *Runtime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.Add(spec)
}

// TaskAddFor 按归属会话登记 task：当前任务会话写实时注册表，后台会话写自身
// scope 分区（R6/P2 收口：写自有域，后台任务不再污染当前注册表）。
func (r *Runtime) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.tasks.Add(spec)
	}
	return r.addTaskPartition(sessionID, spec)
}

// addTaskPartition 把 task 写入指定会话的 scope 分区（幂等：Key 命中返回既有记录）。
//
// 自动 ID 取**进程级**分配器（见 task.NextAutoID）：分区里"每个会话各自从 1
// 数"会让两个会话的无显式 ID 条目同号——跨会话台账按行 ID 去重时丢行，前端
// keyed reconciliation 也只认一行。
func (r *Runtime) addTaskPartition(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	if r.sessionTaskSnapshots == nil {
		r.sessionTaskSnapshots = make(map[string][]dto.TaskRecord)
	}
	records := r.sessionTaskSnapshots[sessionID]
	if spec.Key != "" {
		for _, record := range records {
			if record.Key == spec.Key {
				return record, false, nil
			}
		}
	}
	id := spec.ID
	if id == "" {
		id = task.NextAutoID("task")
		for partitionOccupied(records, id) {
			id = task.NextAutoID("task")
		}
	} else {
		task.ObserveAutoID(id)
	}
	record := dto.TaskRecord{
		ID: id, Key: spec.Key, Phase: spec.Phase, Task: spec.Task, Description: spec.Description,
		Status: dto.TaskPending, Assignee: spec.Assignee, Kind: spec.Kind,
		Dependencies: append([]string(nil), spec.Dependencies...),
		Attachments:  append([]string(nil), spec.Attachments...),
	}
	r.sessionTaskSnapshots[sessionID] = append(records, record)
	return record, true, nil
}

// partitionOccupied 判断分区里是否已有同 ID 行（分区行键同样是 ID）。
func partitionOccupied(records []dto.TaskRecord, id string) bool {
	for _, record := range records {
		if record.ID == id {
			return true
		}
	}
	return false
}

// ResolveTaskByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (r *Runtime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.ResolveByKey(key)
}

// ResolveTaskByKeyFor 按归属会话查 task。
func (r *Runtime) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.tasks.ResolveByKey(key)
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	for _, record := range r.sessionTaskSnapshots[sessionID] {
		if record.Key == key {
			return record, true, nil
		}
	}
	return dto.TaskRecord{}, false, nil
}

// TaskSetStatus 更新 task 状态（生命周期打点）。
func (r *Runtime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.SetStatus(id, status, evidence)
}

// TaskSetStatusFor 按归属会话更新 task 状态。
func (r *Runtime) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("task: registry unavailable")
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.tasks.SetStatus(id, status, evidence)
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	records := r.sessionTaskSnapshots[sessionID]
	for index := range records {
		if records[index].ID != id {
			continue
		}
		record := records[index]
		record.Status = status
		if status == dto.TaskRetry {
			record.RetryCount++
		}
		records[index] = record
		r.sessionTaskSnapshots[sessionID] = records
		return record, nil
	}
	return dto.TaskRecord{}, fmt.Errorf("task %q not found in session %q scope", id, sessionID)
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

// SwitchSessionTasks 会话切换：保存旧会话注册表快照到其 scope 分区，装载
// 目标会话分区记录到当前注册表。sessionID 为空 = 进入草稿（无会话归属）。
// 与旧语义的关键差异：后台会话的 TaskAddFor 分区写不再被切换覆盖（换血
// 只作用于当前注册表本身，分区是写自有域的权威存储）。
//
// 注意：本方法只改变**当前会话**的实时注册表；工作表格走全局读面
// （TaskSnapshot = 实时注册表 + 全部分区），切走/新建都不丢行。
func (r *Runtime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	if r == nil || r.tasks == nil {
		return
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	current := r.tasks.Snapshot()
	if r.currentTaskSessionID != "" && r.currentTaskSessionID != sessionID {
		r.sessionTaskSnapshots[r.currentTaskSessionID] = current
	}
	r.currentTaskSessionID = sessionID
	// 目标会话已装载进当前注册表后，其分区即过期（实时读当前注册表）；
	// 删除避免后续 TaskSnapshotFor 读到陈旧分片。
	if sessionID != "" {
		delete(r.sessionTaskSnapshots, sessionID)
	}
	_ = r.tasks.ReplaceAll(records)
}

// SetSessionWorkspace 记录会话绑定的 workspace ID（application 在恢复/
// 物化/绑定工作区时通知；framework DurableHistory 按显式键落盘，R3 键
// 漂移收敛）。
func (r *Runtime) SetSessionWorkspace(sessionID, workspaceID string) {
	if r == nil {
		return
	}
	r.sessionWorkspacesMu.Lock()
	r.sessionWorkspaces[sessionID] = workspaceID
	r.sessionWorkspacesMu.Unlock()
}

// SetCurrentTaskBatch 设置会话级默认批次（application startChat 按会话
// 调用；后台会话不覆盖活跃注册表默认批次——L3 写隔离）。
func (r *Runtime) SetCurrentTaskBatch(sessionID, batchID string) {
	if r == nil || r.tasks == nil {
		return
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	r.sessionBatchesMu.Lock()
	if r.sessionBatches == nil {
		r.sessionBatches = make(map[string]string)
	}
	r.sessionBatches[sessionID] = batchID
	r.sessionBatchesMu.Unlock()
	if sessionID == current {
		_ = r.tasks.SetDefaultBatch(batchID)
	}
}

// CurrentTaskBatchFor 返回指定会话的默认批次（TaskAdd 盖章辅助；空 =
// 未启动）。
func (r *Runtime) CurrentTaskBatchFor(sessionID string) string {
	if r == nil {
		return ""
	}
	r.sessionBatchesMu.Lock()
	defer r.sessionBatchesMu.Unlock()
	return r.sessionBatches[sessionID]
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

// SetPluginUnassembledReason 注入"未定义插件名怎么判 / 怎么说"的判决函数（产品侧在
// 启动期把**精选目录**的读数包成它；nil = 取消注入，回落既有"未定义"口径）。
//
// 它是插件域的端口（与 DefinePlugin 同族），不是插件面收窄逻辑：判决只影响
// "这个名字收不收"与拒绝文案，不改变任何已装插件的 include/exclude。
func (r *Runtime) SetPluginUnassembledReason(fn func(name string, installed []string) error) {
	if r == nil || r.plugins == nil {
		return
	}
	r.plugins.SetUnassembledReason(fn)
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

// ScheduledPromptOutcome 是一次定时提示词触发的落点（展示文本 + 本次会话号）。
type ScheduledPromptOutcome = scheduler.PromptOutcome

// ScheduledPromptExecutor 提示词任务执行器（main 装配注入；nil = prompt 任务
// 不可创建）。落点口径：sessionID 非空 → 投递该会话；空（默认）→ **新建会话
// 发起**，workspaceID 非空时把新会话装配到该工作区。
type ScheduledPromptExecutor = scheduler.PromptExecutor

// RestoreScheduledTasks 从全局 JSONL 读回定时任务定义（冷启动重建）：返回
// （恢复数, 跳过数）。跳过 = 记录已不适用（一次性已过期/命令不在白名单/
// 周期非法），逐条跳过而不是一条坏记录挡住全部。必须在执行器与观察者注入
// 之后调用——恢复出来的任务会按排期立刻触发。
//
// 存储粒度是**全局**的（`<store>/scheduled-tasks.jsonl`，见 RuntimeConfig
// .ScheduledTasksPath）：任务不按项目分区、不按会话分片；触发产生的会话记录
// 才走会话自己的存储与读写纪律。
func (r *Runtime) RestoreScheduledTasks() (int, int, error) {
	if r == nil || r.scheduler == nil {
		return 0, 0, nil
	}
	return r.scheduler.Restore()
}

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

// UpdateScheduledTask 用一份新定义覆盖既有定时/周期任务（编辑入口；校验与创建
// 同一份判据，运行账目保留）。
func (r *Runtime) UpdateScheduledTask(ctx context.Context, id string, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	if r == nil || r.scheduler == nil {
		return nil, errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.Update(ctx, id, spec)
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

// ── todolist 工具族的会话归属（2026-09-29）────────────────────────────
//
// 工具族（todo_init/add/done/status）与 taskadd 同一口径：写的是**调用它的那个
// 会话**的 scope。子代理看得见清单工具族，而子代理可能在后台会话里跑——若走实时
// 注册表，它的 todo_init 会把**当前视图会话**的清单整表顶掉，行也贴错会话号
// （工作表格会话轴张冠李戴）。清单项在注册表里就是 kind=todo 的 task 行，因此
// 会话级读面与 task 分区同构：分区里按 Kind=="todo" 的**顺序**表达清单顺序。
//
// 空会话键 = 无 ctx 归属（旧调用面）→ 实时注册表，保持既有行为。

// TodoSnapshotFor 按归属会话读清单（工具族按调用会话取数）。
func (r *Runtime) TodoSnapshotFor(sessionID string) []dto.TodoItem {
	if r == nil || r.tasks == nil {
		return nil
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	records := append([]dto.TaskRecord(nil), r.sessionTaskSnapshots[sessionID]...)
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.TodoSnapshot()
	}
	return todoItemsFromRecords(records)
}

// ReplaceTodoFor 按归属会话整体替换清单（todo_init）。
func (r *Runtime) ReplaceTodoFor(sessionID string, items []dto.TodoItem) error {
	if r == nil || r.tasks == nil {
		return errors.New("todolist: unavailable")
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.tasks.ReplaceTodo(items)
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	records := partitionWithoutTodo(r.sessionTaskSnapshots[sessionID])
	for _, item := range items {
		records = append(records, todoRecordFromItem(item))
	}
	r.sessionTaskSnapshots[sessionID] = records
	return nil
}

// AppendTodoFor 按归属会话追加清单项（todo_add；上限与注册表同一口径）。
func (r *Runtime) AppendTodoFor(sessionID string, item dto.TodoItem, limit int) error {
	if r == nil || r.tasks == nil {
		return errors.New("todolist: unavailable")
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.tasks.AppendTodo(item, limit)
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	records := r.sessionTaskSnapshots[sessionID]
	if limit > 0 && len(todoRecordsIn(records)) >= limit {
		return fmt.Errorf("todo_add: list already at limit %d", limit)
	}
	r.sessionTaskSnapshots[sessionID] = append(records, todoRecordFromItem(item))
	return nil
}

// SetTodoStatusFor 按归属会话设置清单项三态（todo_done）。
func (r *Runtime) SetTodoStatusFor(sessionID string, index int, status task.TaskStatus) (dto.TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return dto.TaskRecord{}, errors.New("todolist: unavailable")
	}
	r.sessionTaskMu.Lock()
	current := r.currentTaskSessionID
	r.sessionTaskMu.Unlock()
	if sessionID == "" || sessionID == current {
		return r.tasks.SetTodoStatusByIndex(index, status)
	}
	r.sessionTaskMu.Lock()
	defer r.sessionTaskMu.Unlock()
	records := r.sessionTaskSnapshots[sessionID]
	positions := todoRecordPositions(records)
	if index < 0 || index >= len(positions) {
		return dto.TaskRecord{}, fmt.Errorf("todo_done: index %d out of range (0..%d)", index, len(positions)-1)
	}
	position := positions[index]
	if status != task.TaskPending && status != task.TaskDoing && status != task.TaskCompleted {
		return dto.TaskRecord{}, fmt.Errorf("task: todo 只支持三态 pending/doing/completed，不能迁移到 %s", status)
	}
	records[position].Status = status
	r.sessionTaskSnapshots[sessionID] = records
	return records[position], nil
}

// todoItemsFromRecords 还原分区里的清单（按行顺序 = 清单顺序）。
func todoItemsFromRecords(records []dto.TaskRecord) []dto.TodoItem {
	items := make([]dto.TodoItem, 0, len(records))
	for _, record := range records {
		if record.Kind != "todo" {
			continue
		}
		items = append(items, task.TaskToTodoItem(record))
	}
	return items
}

// todoRecordsIn 返回分区里的清单行（保持顺序）。
func todoRecordsIn(records []dto.TaskRecord) []dto.TaskRecord {
	todos := make([]dto.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == "todo" {
			todos = append(todos, record)
		}
	}
	return todos
}

// todoRecordPositions 返回清单行在分区里的下标（顺序即清单顺序）。
func todoRecordPositions(records []dto.TaskRecord) []int {
	positions := make([]int, 0, len(records))
	for index, record := range records {
		if record.Kind == "todo" {
			positions = append(positions, index)
		}
	}
	return positions
}

// partitionWithoutTodo 去掉分区里的清单行（整体替换清单用；任务行原样保留）。
func partitionWithoutTodo(records []dto.TaskRecord) []dto.TaskRecord {
	kept := make([]dto.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == "todo" {
			continue
		}
		kept = append(kept, record)
	}
	return kept
}

// todoRecordFromItem 由清单项构造 kind=todo 的分区行：形状与注册表建的行同构
// （Phase=tasklist、Key=todo:<文本>、状态三态映射），ID 取**进程级**分配器——
// 分区里"每个会话各自从 1 数"会让跨会话台账同号去重丢行（见 addTaskPartition）。
func todoRecordFromItem(item dto.TodoItem) dto.TaskRecord {
	id := task.NextAutoID("todo")
	return dto.TaskRecord{
		ID: id, Key: "todo:" + item.Text, Phase: dto.TaskPhaseTasklist,
		Task: item.Text, Status: task.TodoToTaskStatus(item.Status), Kind: "todo",
	}
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
