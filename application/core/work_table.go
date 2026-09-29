package core

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/worktable"
)

// ── 工作表格（Work Table）投影 ──────────────────────────────
// task 就是 worktable 条目：单一 task 注册表（seelebridge/task_registry.go，
// Actor + Mailbox，保护粒度=task）是权威状态源；plan/subagent 节点生命周期
// 被动同步进注册表（B5），todolist 融合为 kind=todo 的 task（B0），主动
// taskadd 直接入注册表（B1 幂等）。
//
// 事件（B2）：task 内部变更 → task.changed（逐任务增量，脏标记驱动）；
// worktable 结构 → worktable.changed（整表，CSP 汇聚 latest-wins）。
// retry（B3）：status=retry + RetryCount，前端展示 RETRY n。

// buildWorkTable 组装工作表格行：注册表 task → WorkItem；plan 行额外合并
// 节点事件/工具活动打点（详情数据面仍直接读 plan 节点）；后台命令（asyncRuns）
// 以**只读投影**并入，不进注册表（见 work_table_async.go 与不变量 I-21）。
// 有界：行数 ≤ limits.work_table_rows，trace ≤ workTableTraceLimit。
func buildWorkTable(plan *PlanState, tasks []dto.TaskRecord, subagentTree []dto.SubAgentTreeNode, asyncRuns []dto.AsyncRunRecord) []WorkItem {
	nodeByID := make(map[string]PlanNode)
	if plan != nil {
		var walk func(nodes []PlanNode)
		walk = func(nodes []PlanNode) {
			for _, node := range nodes {
				nodeByID[node.ID] = node
				walk(node.Children)
			}
		}
		walk(plan.Nodes)
	}
	phaseOrder := map[string]int{"plan": 0, "task": 1, "tasklist": 2, "subagent": 3}
	rows := make([]WorkItem, 0, len(tasks))
	for _, record := range tasks {
		item := taskRecordToWorkItem(record)
		if record.Kind == "plan" {
			if node, ok := nodeByID[planNodeIDFor(record)]; ok {
				item.Trace = boundWorkTrace(append(item.Trace, planNodeTrace(node, plan != nil && plan.Status != PlanRunning)...))
				// plan 行依赖取自 plan 邻接面（plan_load 产出的是平铺节点表 +
				// 边集，注册表按树形父子推导会让 plan 行的「依赖」列恒为空）。
				item.Dependencies = mergeWorkDependencies(item.Dependencies, planDependencies(plan, node.ID))
			}
		}
		rows = append(rows, item)
	}
	rows = append(rows, asyncWorkItems(asyncRuns)...)
	sort.SliceStable(rows, func(left, right int) bool {
		leftOrder := phaseOrder[rows[left].Phase]
		rightOrder := phaseOrder[rows[right].Phase]
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return rows[left].ID < rows[right].ID
	})
	if limit := Limits().WorkTableRows; limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// taskRecordToWorkItem 把注册表 task 快照映射为 WorkItem（含 retry 计数）。
func taskRecordToWorkItem(record dto.TaskRecord) WorkItem {
	trace := make([]WorkTracePoint, 0, len(record.Trace))
	for _, point := range record.Trace {
		trace = append(trace, WorkTracePoint{
			At: point.At, Status: point.Status, Operation: point.Operation,
			Evidence: truncateWorkEvidence(point.Evidence, Limits().EvidenceChars), Duration: point.Duration,
		})
	}
	trace = boundWorkTrace(trace)
	status := string(record.Status)
	if record.Kind == "todo" && record.Status == dto.TaskCompleted {
		// todo 三态契约：done（前端状态按钮 active 判定兼容）。
		status = "done"
	}
	if record.Kind == "subagent" && record.Status == dto.TaskCompleted {
		// 子代理阶段沿用 running/done/failed 语义。
		status = "done"
	}
	return WorkItem{
		ID: record.ID, SessionID: record.SessionID, Phase: record.Phase,
		Task:        truncateWorkEvidence(record.Task, 200),
		Description: truncateWorkEvidence(record.Description, Limits().EvidenceChars),
		Status:      status, RetryCount: record.RetryCount, Assignee: record.Assignee,
		Dependencies: append([]string(nil), record.Dependencies...),
		Attachments:  append([]string(nil), record.Attachments...),
		Kind:         record.Kind, SourceID: record.SourceID,
		Participants: append([]string(nil), record.Participants...),
		BatchID:      record.BatchID,
		BatchLabel:   batchLabel(record.BatchID, record.CreatedAt),
		CreatedAt:    record.CreatedAt,
		StartedAt:    record.StartedAt, EndedAt: record.EndedAt, Elapsed: record.Elapsed,
		Trace: trace,
	}
}

// legacyBatchID 是早期/未分批会话的伪批次 ID（空串；展示标签「早期任务」，
// 排序置底，不与真实 chat 请求批次混排）。
const legacyBatchID = ""

// batchLabel 由批次 ID 与创建时间派生展示标签：真实批次用本地时间
// （YYYY-MM-DD HH:MM），早期/未分批会话固定为「早期任务」。
func batchLabel(id string, createdAt time.Time) string {
	if id == "" {
		return "早期任务"
	}
	if createdAt.IsZero() {
		return id
	}
	return createdAt.Format("2006-01-02 15:04")
}

// buildWorkTableBatches 从工作表格行派生批次分片头：按 BatchID 分组，
// 组内取最早 CreatedAt 作为批次时间；按 CreatedAt 升序返回，空批次（早期
// 会话）置底。Counts 按权威类型统计（all/plan/task/todo/subagent），
// 仅包含有内容的批次。
func buildWorkTableBatches(rows []WorkItem) []WorkTableBatch {
	type batchGroup struct {
		id        string
		createdAt time.Time
		counts    map[string]int
	}
	order := make([]string, 0, 4)
	byID := make(map[string]*batchGroup)
	for _, row := range rows {
		id := row.BatchID
		group := byID[id]
		if group == nil {
			group = &batchGroup{
				id: id,
				counts: map[string]int{
					"all": 0, "plan": 0, "task": 0, "todo": 0, "subagent": 0,
				},
			}
			byID[id] = group
			order = append(order, id)
		}
		group.counts["all"]++
		group.counts[row.Kind]++
		if group.createdAt.IsZero() || (!row.CreatedAt.IsZero() && row.CreatedAt.Before(group.createdAt)) {
			group.createdAt = row.CreatedAt
		}
	}
	batches := make([]WorkTableBatch, 0, len(order))
	for _, id := range order {
		group := byID[id]
		batches = append(batches, WorkTableBatch{
			ID:        group.id,
			Label:     batchLabel(group.id, group.createdAt),
			CreatedAt: group.createdAt,
			Counts:    group.counts,
		})
	}
	sort.SliceStable(batches, func(left, right int) bool {
		leftLegacy := batches[left].ID == legacyBatchID
		rightLegacy := batches[right].ID == legacyBatchID
		if leftLegacy != rightLegacy {
			return !leftLegacy // 早期/未分批置底
		}
		leftAt, rightAt := batches[left].CreatedAt, batches[right].CreatedAt
		if leftAt.IsZero() != rightAt.IsZero() {
			return !leftAt.IsZero() // 无时间的批次靠后
		}
		if !leftAt.Equal(rightAt) {
			return leftAt.Before(rightAt)
		}
		return batches[left].ID < batches[right].ID
	})
	return batches
}

// planNodeTrace 由节点事件 + 子代理工具活动合成打点（按时间倒序、有界；
// 详情弹窗数据面仍直接读 plan 节点，此处只是工作表格概览合并）。
func planNodeTrace(node PlanNode, tasklistMode bool) []WorkTracePoint {
	points := make([]WorkTracePoint, 0, len(node.Events)+len(node.ToolEvents))
	for _, event := range node.Events {
		operation := "node.lifecycle"
		if tasklistMode && event.Status == NodeCompleted {
			operation = "task_check_node"
		}
		points = append(points, WorkTracePoint{
			At: event.At, Status: string(event.Status), Operation: operation,
			Evidence: truncateWorkEvidence(event.Output, Limits().EvidenceChars),
		})
	}
	for _, tool := range node.ToolEvents {
		evidence := tool.Error
		if evidence == "" {
			evidence = tool.Result
		}
		if evidence == "" {
			evidence = tool.Arguments
		}
		points = append(points, WorkTracePoint{
			At: tool.StartedAt, Status: tool.Status, Operation: tool.Name,
			Evidence: truncateWorkEvidence(evidence, Limits().EvidenceChars),
			Duration: formatWorkDuration(tool.Duration),
		})
	}
	return points
}

// workTableTraceLimit 是工作表格概览的行内打点上限（完整时间线在详情弹窗）。
const workTableTraceLimit = 10

// boundWorkTrace 按时间倒序排序并截断。
func boundWorkTrace(points []WorkTracePoint) []WorkTracePoint {
	sort.SliceStable(points, func(left, right int) bool {
		return points[left].At.After(points[right].At)
	})
	if limit := Limits().PlanNodeEvents; limit > 0 && len(points) > limit {
		points = points[:limit]
	}
	if len(points) > workTableTraceLimit {
		points = points[:workTableTraceLimit]
	}
	return points
}

func truncateWorkEvidence(value string, limit int) string {
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

func formatWorkDuration(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	ms := float64(duration) / float64(time.Millisecond)
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// refreshWorkTableLocked 在 service.ViewMu 持锁时重建工作表格投影。
//
// 两个输入都必须是**锁外采样**后按值传进来的：任务注册表快照与后台作业投影都要读
// 宿主，而后台作业投影会对每条记录做 stat + 读日志末窗（文件 I/O）。持进程级视图
// 写锁采样 = 把整块交互面押在一次慢活上（2026-09-29 锁面审计 §2.5，与 2026-09-29
// 折叠持 ViewMu 推帧的"后果②"同形）。
func (state *serviceState) refreshWorkTableLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord) {
	rows := buildWorkTable(
		state.Snapshot.Runtime.Plan,
		tasks,
		state.Snapshot.Runtime.SubAgentTree,
		asyncRuns,
	)
	state.Snapshot.Runtime.WorkTable = rows
	state.Snapshot.Runtime.WorkTableBatches = buildWorkTableBatches(rows)
}

// publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
// 是同一临界区克隆的不可变快照，保证 revision 与内容一致）。
func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []WorkItem, batches []WorkTableBatch) {
	if state.workTablePublisher == nil {
		return
	}
	state.workTablePublisher.Send(worktable.WorkTableUpdate{
		Revision: revision, RequestID: requestID, Items: items, Batches: batches,
	})
}

// workTableEventPayload 组装 worktable.changed 的 payload：表格 + 批次头 +
// 子代理树增量。
//
// 树只在内容变化时随包下发（nil = 不带、空数组 = 已清空）：详情入口在前端
// 要先把行解析成节点（Plan DSL / 子代理树投影），而树此前只由整份快照与
// runtime.changed 携带；同一批次还有子代理在跑时，表格行已通过本事件到达、
// 树却还没到，详情点开会静默失败（2026-09-13 回归）。比较基准是"上一次
// 实际发布出去的树"，因此被 CSP 汇聚合并掉的中间更新不会漏发。
//
// 只由 workTablePublisher 的发布 goroutine 调用：lastPublishedSubagentTreeSig
// 因此无需额外加锁（发布器是单 goroutine）。
func (service *Service) workTableEventPayload(update worktable.WorkTableUpdate) WorkTableEvent {
	service.ViewMu.RLock()
	tree := cloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	service.ViewMu.RUnlock()
	payload := WorkTableEvent{Items: update.Items, Batches: update.Batches}
	signature := subagentTreePayloadSignature(tree)
	if signature == service.lastPublishedSubagentTreeSig {
		return payload
	}
	service.lastPublishedSubagentTreeSig = signature
	if tree == nil {
		// 显式空数组：前端据此清空既有树（nil/缺省 = 本次不带，保留）。
		tree = []dto.SubAgentTreeNode{}
	}
	payload.SubAgentTree = tree
	return payload
}

// subagentTreePayloadSignature 生成子代理树投影的内容签名，用于判断
// worktable.changed 是否需要随包携带树。签名覆盖身份/状态/会话号与各文本
// 字段、上下文计数的长度，既能识别投影内容变化，也避免为比较把整棵树
// 序列化一遍（树是运行期唯一较重的投影，表格增量本身要保持轻量）。
func subagentTreePayloadSignature(nodes []dto.SubAgentTreeNode) string {
	var builder strings.Builder
	var walk func(items []dto.SubAgentTreeNode)
	walk = func(items []dto.SubAgentTreeNode) {
		fmt.Fprintf(&builder, "%d:", len(items))
		for _, node := range items {
			fmt.Fprintf(&builder, "%s|%s|%s|%d|%d|%d|%d|%d;",
				node.ID, node.Status, node.SessionID,
				len(node.Goal), len(node.Summary), len(node.Error),
				node.StartedAt.UnixNano(), node.EndedAt.UnixNano())
			if node.Context != nil {
				fmt.Fprintf(&builder, "ctx:%d|%d|%d|%d|%d|",
					len(node.Context.Goal), node.Context.MessageCount, node.Context.TokenEstimate,
					len(node.Context.Progress), len(node.Context.Findings))
				for _, finding := range node.Context.Findings {
					fmt.Fprintf(&builder, "%d|", len(finding))
				}
			}
			builder.WriteString(";")
			walk(node.Children)
		}
	}
	walk(nodes)
	return builder.String()
}

// publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
// payload 小，逐任务保证不丢）。事件携带归属会话 sid（前端按会话过滤，
// 后台注册表变更不污染当前视图）。
func (service *Service) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string) {
	if service.Events == nil {
		return
	}
	// 归属会话补齐（与整表路径同源）：增量只源自实时注册表（后台分区写不发
	// 增量，见 seelebridge TaskAddFor/TaskSetStatusFor），而实时注册表恒属于
	// 当前任务会话；注册表记录本身不带 SessionID，整表投影才给它标
	// currentTaskSessionID。不补齐时整表已带键的行会被这份无键增量整行替换
	// （前端 protocol.js 按 task_id 整行覆盖），前端"仅本会话"筛选随即丢掉
	// 正在运行的行（子代理任务每状态迁移都发增量，最容易撞上）。
	if record.SessionID == "" {
		record.SessionID = sessionID
	}
	// 与整表投影同源补齐（plan 行依赖取自 plan 邻接面）：否则 task.changed
	// 会把该行的依赖列擦成空。
	item := service.workItemForRecord(record, sessionID)
	service.publishSessionEvent(EventTaskChanged, revision, requestID, sessionID, TaskChangedEvent{TaskID: item.ID, Task: item})
}

// publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
// worktable.changed（结构安全网）；task.changed 由 CSP 消费者直发。
func (service *Service) publishTaskDeltas() {
	tasks := service.Deps.Runtime.TaskSnapshot()
	asyncRuns := service.asyncRunsForTable()
	service.ViewMu.Lock()
	service.refreshWorkTableLocked(tasks, asyncRuns)
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	items := CloneWorkItems(service.Core.Snapshot.Runtime.WorkTable)
	batches := CloneWorkTableBatches(service.Core.Snapshot.Runtime.WorkTableBatches)
	service.ViewMu.Unlock()
	service.publishWorkTable(revision, requestID, items, batches)
}

// syncTasksFromSources 同步当前活跃会话的 plan/子代理树到其自身 task scope。
func (service *Service) syncTasksFromSources() {
	service.ViewMu.RLock()
	sid := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.syncTasksFromSourcesFor(sid)
}

// syncTasksFromSourcesFor 把指定会话的 plan 节点与子代理树生命周期投影进该
// 会话自身的 task scope（R6/P2 收口：写自有域，后台会话不再污染当前注册表）。
func (service *Service) syncTasksFromSourcesFor(sessionID string) {
	service.ViewMu.RLock()
	plan := service.sessionActivePlanLocked(sessionID)
	tree := cloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	// 活跃会话的 task scope 恒为实时注册表（""）；后台会话写自身分区。
	scope := sessionID
	if sessionID == "" || sessionID == service.Core.Snapshot.Session.ID {
		scope = ""
	}
	service.ViewMu.RUnlock()

	if plan != nil {
		var walk func(nodes []PlanNode, parentID string)
		walk = func(nodes []PlanNode, parentID string) {
			for _, node := range nodes {
				service.syncPlanNodeTask(scope, node, parentID)
				walk(node.Children, "plan:"+node.ID)
			}
		}
		walk(plan.Nodes, "")
	}
	var walkTree func(items []dto.SubAgentTreeNode, parentID string)
	walkTree = func(items []dto.SubAgentTreeNode, parentID string) {
		for _, node := range items {
			if node.ID == "" || node.ID == "main" {
				walkTree(node.Children, parentID)
				continue
			}
			service.syncSubagentTask(scope, node, parentID)
			walkTree(node.Children, node.ID)
		}
	}
	walkTree(tree, "")
}

// sessionActivePlanLocked 返回指定会话当前 plan 投影：活跃会话读 Snapshot 镜像，
// 后台会话读会话自身的 plan 栈（task_context 每会话持有）。
func (service *Service) sessionActivePlanLocked(sessionID string) *PlanState {
	if sessionID == service.Core.Snapshot.Session.ID {
		return clonePlanForSync(service.Core.Snapshot.Runtime.Plan)
	}
	return task_context.ActivePlanFromStack(
		service.components.tasks.PlanStackFor(sessionID),
		service.components.tasks.ActivePlanIDFor(sessionID),
	)
}

func (service *Service) syncPlanNodeTask(sessionID string, node PlanNode, parentID string) {
	key := "plan:" + node.ID
	status := taskStatusForNode(node.Status)
	identity := dto.ActorIdentity("main", service.Deps.Engine.SessionID())
	existing, found, err := service.Deps.Runtime.ResolveTaskByKeyFor(sessionID, key)
	if err != nil {
		return
	}
	if !found {
		spec := dto.TaskSpec{
			ID: key, Key: key, Phase: dto.TaskPhasePlan, Task: node.Label, Kind: "plan",
			SourceID: node.ID, Assignee: identity,
		}
		if parentID != "" {
			spec.Dependencies = []string{parentID}
		}
		_, _, _ = service.Deps.Runtime.TaskAddFor(sessionID, spec)
		return
	}
	if existing.Status != status {
		_, _ = service.Deps.Runtime.TaskSetStatusFor(sessionID, existing.ID, status, "node:"+string(node.Status))
	}
	// 被动认领：旧数据/恢复会话无 Assignee 时补主身份并上名单。
	if existing.Assignee == "" && identity != "" {
		_, _ = service.Deps.Runtime.TaskAttachParticipant(existing.ID, identity)
	}
}

func (service *Service) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string) {
	key := "subagent:" + node.ID
	status := taskStatusForSubagent(node.Status)
	identity := dto.ActorIdentity("subagent", node.SessionID)
	existing, found, err := service.Deps.Runtime.ResolveTaskByKeyFor(sessionID, key)
	if err != nil {
		return
	}
	if !found {
		spec := dto.TaskSpec{
			ID: key, Key: key, Phase: dto.TaskPhaseSubagent, Task: node.Goal, Kind: "subagent",
			SourceID: node.ID,
		}
		if parentID != "" {
			spec.Dependencies = []string{"subagent:" + parentID}
		}
		created, _, _ := service.Deps.Runtime.TaskAddFor(sessionID, spec)
		_, _ = service.Deps.Runtime.TaskSetStatusFor(sessionID, created.ID, status, "subagent:"+string(node.Status))
		// 会话已注册 → 子代理认领（Assignee 变更为 subagent:<sessionID> 并上名单）。
		if identity != "" {
			_, _ = service.Deps.Runtime.TaskAttachParticipant(created.ID, identity)
		}
		return
	}
	if existing.Status != status {
		_, _ = service.Deps.Runtime.TaskSetStatusFor(sessionID, existing.ID, status, "subagent:"+string(node.Status))
	}
	// 被动认领：确保当前子代理身份在名单上并成为 Assignee。
	if identity != "" {
		_, _ = service.Deps.Runtime.TaskAttachParticipant(existing.ID, identity)
	}
}

func taskStatusForNode(status NodeStatus) dto.TaskStatus {
	switch status {
	case NodeRunning, NodeWorktreeCreating, NodeRebasing, NodeMerging, NodeQueued:
		return dto.TaskRunning
	case NodeCompleted, NodeSkipped:
		return dto.TaskCompleted
	case NodeFailed, NodePanicked:
		return dto.TaskFailed
	case NodeAborted, NodeCanceled:
		return dto.TaskFailed
	default:
		return dto.TaskPending
	}
}

func taskStatusForSubagent(status dto.SubAgentNodeStatus) dto.TaskStatus {
	switch status {
	case dto.SubAgentQueued:
		return dto.TaskQueued
	case dto.SubAgentRunning:
		return dto.TaskRunning
	case dto.SubAgentDone:
		return dto.TaskCompleted
	case dto.SubAgentFailed:
		return dto.TaskFailed
	case dto.SubAgentInterrupted:
		return dto.TaskInterrupted
	default:
		return dto.TaskInterrupted
	}
}

// planNodeIDFor 返回 plan 行对应的 plan 节点 ID：SourceID 优先，缺失时
// 回退 ID 的 plan: 前缀（恢复会话/老数据可能没有 SourceID，而 plan:<nodeID>
// 是工作表格行的权威键）。
func planNodeIDFor(record dto.TaskRecord) string {
	if record.SourceID != "" {
		return record.SourceID
	}
	if nodeID, ok := strings.CutPrefix(record.ID, "plan:"); ok {
		return nodeID
	}
	return ""
}

// planDependencies 由 plan 邻接面取节点的前置任务 ID（plan:<前置节点>）。
//
// plan 的依赖是 **DAG 入边**（plan_load 的 edges），不是树形父子关系：
// plan_load 产出的是平铺节点表 + 边集，按父子推导会让所有 plan 行的依赖
// 恒为空——工作表格「依赖」列对 plan 行不可见（用户看不到自己在等谁）。
func planDependencies(plan *PlanState, nodeID string) []string {
	if plan == nil || nodeID == "" || len(plan.Edges) == 0 {
		return nil
	}
	deps := make([]string, 0, 2)
	for _, edge := range plan.Edges {
		if edge.To != nodeID || edge.From == "" {
			continue
		}
		deps = append(deps, "plan:"+edge.From)
	}
	return deps
}

// mergeWorkDependencies 合并依赖来源（注册表记录 + plan 邻接面）：去重、
// 稳定排序（UI 与用例的确定性），空结果返回 nil（JSON omitempty 面）。
func mergeWorkDependencies(recorded []string, derived []string) []string {
	if len(derived) == 0 {
		return recorded
	}
	merged := make([]string, 0, len(recorded)+len(derived))
	seen := make(map[string]struct{}, len(recorded)+len(derived))
	for _, source := range [][]string{recorded, derived} {
		for _, dependency := range source {
			if dependency == "" {
				continue
			}
			if _, exists := seen[dependency]; exists {
				continue
			}
			seen[dependency] = struct{}{}
			merged = append(merged, dependency)
		}
	}
	sort.Strings(merged)
	return merged
}

// mergeTaskRecords 合并两组 task 记录（按跨会话身份去重，前者优先，稳定
// 排序）：冷读面用全局表 + 会话持久化的自身条目拼一张表，保证"任何入口看到
// 的都是同一条跨会话台账"。身份 = 幂等键优先、否则行 ID：同一 plan 节点/
// 子代理/待办文本在不同来源（实时注册表、会话分区、磁盘记录）出现时靠 Key
// 认同一行；自动行 ID（`task:<n>`/`todo:<n>`）由进程级分配器保证进程内唯一，
// 不会把两个会话的条目并成一行（见 seelebridge/task.NextAutoID）。
//
// secondarySessionID 标注 secondary 的归属会话：磁盘记录不带会话标记（归属
// 由容器 `SessionRecord` 表达），冷读时把该会话的持久化条目补上标记，会话
// 筛选轴才不会把它们的行判成"无主"。
func mergeTaskRecords(primary, secondary []dto.TaskRecord, secondarySessionID string) []dto.TaskRecord {
	if len(secondary) == 0 {
		return primary
	}
	merged := make([]dto.TaskRecord, 0, len(primary)+len(secondary))
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	appendRecord := func(record dto.TaskRecord) {
		identity := taskRecordLedgerIdentity(record)
		if identity == "" {
			return
		}
		if _, exists := seen[identity]; exists {
			return
		}
		seen[identity] = struct{}{}
		merged = append(merged, record)
	}
	for _, record := range primary {
		appendRecord(record)
	}
	for _, record := range secondary {
		if record.SessionID == "" {
			record.SessionID = secondarySessionID
		}
		appendRecord(record)
	}
	sort.SliceStable(merged, func(left, right int) bool { return merged[left].ID < merged[right].ID })
	return merged
}

// taskRecordLedgerIdentity 返回记录在跨会话台账里的去重身份（幂等键优先、
// 否则行 ID；与 seelebridge 侧的 taskLedgerIdentity 同口径）。
func taskRecordLedgerIdentity(record dto.TaskRecord) string {
	if record.Key != "" {
		return "key:" + record.Key
	}
	if record.ID != "" {
		return "id:" + record.ID
	}
	return ""
}

// workItemForRecord 把注册表记录映射为工作表格行，并按 plan 邻接面补齐
// plan 行的依赖。
//
// task.changed 增量与 worktable.changed 整表必须同源：增量只带单条记录
// （没有 plan 上下文），若不补齐，前端按增量更新该行时会把依赖列擦成空。
func (service *Service) workItemForRecord(record dto.TaskRecord, sessionID string) WorkItem {
	item := taskRecordToWorkItem(record)
	if record.Kind != "plan" {
		return item
	}
	service.ViewMu.RLock()
	plan := service.sessionActivePlanLocked(sessionID)
	service.ViewMu.RUnlock()
	item.Dependencies = mergeWorkDependencies(item.Dependencies, planDependencies(plan, planNodeIDFor(record)))
	return item
}

// ── 工作打点表（上下文标记块）─────────────────────────────
// 动态任务状态不进 system prompt（避免前缀缓存失效），改由“打点表”承载：
// 请求尾部一段用标记语言锁住的只读块，随任务打点增量更新，任务终态后
// 删除（打点完即删），且不落历史 → 天然不参与上下文压缩。

const (
	workTableTraceMarkerOpen  = "<!-- seelex:worktable:v1 -->"
	workTableTraceMarkerClose = "<!-- /seelex:worktable:v1 -->"
	workTableTraceMaxLines    = 30
)

// workTableTraceBlock 返回当前活跃会话的打点表标记块（活跃会话即
// TaskSnapshotFor("") 的实时注册表语义；供旧调用面与进程级事件兼容，
// 多会话路径一律走 workTableTraceBlockFor）。
func (state *serviceState) workTableTraceBlock() string {
	return state.workTableTraceBlockFor("")
}

// workTableTraceBlockFor 返回指定会话的打点表标记块：只含该会话 scope 中
// 未终态任务（pending/running/doing/retry）**与在跑的后台命令**，按稳定顺序；
// 两者都没有时返回空串（块随任务完成/命令收尾自动消失）。
//
// 会话作用域（S1：打点表按会话取数）：打点表**不是**工作表格本体——工作
// 表格是全局台账（TaskSnapshot），而打点表注入在“正在组装下一次请求的会话”
// 的上下文尾部，必须只取该会话自己的 scope，否则会话会读到别的会话的
// 活动任务（历史污染/上下文串台）且看不到自己的打点：
//   - 活跃（视图）会话 = TaskSnapshotFor("")，即实时注册表（runtime.
//     SwitchSessionTasks 在 BeginNewSession/Resume/Activate 时与视图对齐，
//     live registry 恒属于当前视图会话）；
//   - 后台会话（视图已切走、它仍在并行跑） = TaskSnapshotFor(sessionID) 的
//     scope 分区。
//
// 后台执行不走那套分区：登记表里每条都自带归属会话键，按 effectiveSession 过滤
// 即可（"" 表示视图会话，用快照里的会话 ID 顶上）。
func (state *serviceState) workTableTraceBlockFor(sessionID string) string {
	if state == nil || state.Deps.Runtime == nil {
		return ""
	}
	state.ViewMu.RLock()
	viewID := state.Snapshot.Session.ID
	state.ViewMu.RUnlock()
	effectiveSession := sessionID
	if effectiveSession == "" || effectiveSession == viewID {
		effectiveSession = viewID
	}
	var records []dto.TaskRecord
	if forTasks, ok := state.Deps.Runtime.(interface{ TaskSnapshotFor(string) []dto.TaskRecord }); ok {
		scope := sessionID
		if scope == "" || scope == viewID {
			// 视图/活跃会话的 task scope 恒为实时注册表（"" 分区）。
			scope = ""
		}
		records = forTasks.TaskSnapshotFor(scope)
	} else {
		records = state.Deps.Runtime.TaskSnapshot()
	}
	active := make([]dto.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Status != dto.TaskCompleted && record.Status != dto.TaskFailed {
			active = append(active, record)
		}
	}
	sort.SliceStable(active, func(left, right int) bool {
		return active[left].ID < active[right].ID
	})
	lines := make([]string, 0, len(active)+1)
	for _, record := range active {
		retry := ""
		if record.RetryCount > 0 {
			retry = fmt.Sprintf(" retry=%d", record.RetryCount)
		}
		lines = append(lines, fmt.Sprintf("- %s %s%s %s",
			record.ID, record.Status, retry, truncateWorkEvidence(record.Task, 80)))
	}
	asyncLines := asyncTraceLines(state.Deps.Runtime.AsyncRunsSnapshot(), effectiveSession)
	lines = append(lines, asyncLines...)
	if len(lines) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(workTableTraceMarkerOpen + "\n# 工作打点表（系统维护，只读；任务状态与打点以工作表格为准）\n")
	// 预算必须把**整块**算进去（开/闭标记 + 标题 + 读法说明），否则"块 ≤ 30 行"这条
	// 上限会被固定开销吃掉几行（打点 K-2 的判据是整块行数）。
	budget := workTableTraceMaxLines - 3 // 开标记 + 标题 + 闭标记
	hint := ""
	if len(asyncLines) > 0 {
		hint = "async:<句柄> 行是作业（bash_bg 后台命令 / read_batch 扇出读 / subagent）：" +
			"字节数=已产出输出量；在途行带标题，完成行带摘要（exit/行数/末行采样）。" +
			"取结果用 job_manage(op=fetch, handle)、只读看进展用 op=observe、终止用 op=kill、" +
			"销项用 op=done。"
		budget--
	}
	truncated := false
	if len(lines) > budget {
		// 提示行也占一格：截断要让模型看见"还有更多"，而不是静默变短。
		lines = lines[:budget-1]
		truncated = true
	}
	for _, line := range lines {
		builder.WriteString(line + "\n")
	}
	if truncated {
		builder.WriteString("- …（打点表已达上限，详情见工作表格）\n")
	}
	if hint != "" {
		builder.WriteString(hint + "\n")
	}
	builder.WriteString(workTableTraceMarkerClose)
	return builder.String()
}

func clonePlanForSync(plan *PlanState) *PlanState {
	if plan == nil {
		return nil
	}
	return cloneRuntimeState(RuntimeState{Plan: plan}).Plan
}

func cloneSubAgentTreeForSync(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]dto.SubAgentTreeNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = cloneSubAgentTreeForSync(nodes[index].Children)
	}
	return cloned
}

// RefreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
// 消费者 consumeSubagentLifecycle 驱动）：fork 注册/完成自动同步 task 注册表
// 并发布增量，不依赖模型主观意愿调用任何工具。
func (service *Service) RefreshWorkTableSnapshot() {
	// 锁外取子代理树：Engine.SubAgentTree() 会对运行中子代理做 ExportSnapshot
	// （拿子代理会话锁），不能在持 service.ViewMu 时调用——避免与 runner 持会话
	// 锁 → 注册表 actor → 变更 channel 的路径成环死锁。
	tree := service.Deps.Engine.SubAgentTree()
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.SubAgentTree = tree
	service.ViewMu.Unlock()
	service.refreshWorkTableFromSources()
}

// refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
// 注册表，再发布 worktable.changed + task.changed。
func (service *Service) refreshWorkTableFromSources() {
	service.syncTasksFromSources()
	service.publishTaskDeltas()
}

// ── CSP 生命周期消费者（取代同步回调 observer）────────────

// startLifecycleConsumers 启动四个消费者 goroutine：子代理树信号 → 刷新
// 工作表格；plan 节点事件 → 投影；task 变更 → 直发 task.changed；后台执行表变化 →
// 重投影。数据经 channel（CSP）流转，runtime 侧不再同步回调进 application。
func (service *Service) startLifecycleConsumers() {
	if service.Deps.Runtime == nil {
		return
	}
	if service.lifecycleStop == nil {
		service.lifecycleStop = make(chan struct{})
	}
	go service.consumeSubagentLifecycle()
	go service.consumePlanNodeEvents()
	go service.consumeTaskChanges()
	go service.consumeAsyncRuns()
}

func (service *Service) stopLifecycleConsumers() {
	if service.lifecycleStop != nil {
		service.lifecycleOnce.Do(func() { close(service.lifecycleStop) })
	}
}

func (service *Service) consumeSubagentLifecycle() {
	events := service.Deps.Runtime.SubagentTreeEvents()
	if events == nil {
		return
	}
	for {
		select {
		case <-events:
			service.safeLifecycleCall(service.RefreshWorkTableSnapshot)
		case <-service.lifecycleStop:
			return
		}
	}
}

func (service *Service) consumePlanNodeEvents() {
	events := service.Deps.Runtime.PlanNodeEventChannel()
	if events == nil {
		return
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			service.safeLifecycleCall(func() { service.HandlePlanNodeComplete(event) })
		case <-service.lifecycleStop:
			return
		}
	}
}

func (service *Service) consumeTaskChanges() {
	changes := service.Deps.Runtime.TaskChangedChannel()
	if changes == nil {
		return
	}
	for {
		select {
		case record, ok := <-changes:
			if !ok {
				return
			}
			service.ViewMu.RLock()
			revision := service.Core.Snapshot.Revision
			requestID := service.Core.Snapshot.Chat.RequestID
			sessionID := service.Core.Snapshot.Session.ID
			service.ViewMu.RUnlock()
			service.safeLifecycleCall(func() { service.publishTaskChanged(record, revision, requestID, sessionID) })
		case <-service.lifecycleStop:
			return
		}
	}
}

// consumeAsyncRuns 是第四个生命周期消费者：后台执行表一有可见变化（派发、终态、
// 驱逐、以及去抖后的"有新字节"）就重投影工作表格。
//
// 为什么走 refreshWorkTableFromSources 而不是自己发一种新事件：后台行只是整表的
// 一路输入，复用同一条发布路径才能保证"表格里看到的"与"上下文打点块里看到的"
// 永远同源、同一个 revision。信号是容量 1 的汇聚口，被合并掉的中间态无所谓——
// 每次重投影读的都是登记表当下全量。
func (service *Service) consumeAsyncRuns() {
	events := service.Deps.Runtime.AsyncRunEvents()
	if events == nil {
		return
	}
	for {
		select {
		case <-events:
			service.safeLifecycleCall(service.refreshWorkTableFromSources)
		case <-service.lifecycleStop:
			return
		}
	}
}

// safeLifecycleCall 处理消费者中的 panic：数据竞争/逻辑故障不得静默吞掉
// 继续运行（可能持续污染其它会话）——先降级退出，再重新抛出 panic 让宿主
// 及时退出（fail-fast）。死锁类问题由带超时的探针用例负责超时即失败。
func (service *Service) safeLifecycleCall(call func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			service.handleLifecycleFault(recovered)
		}
	}()
	call()
}

// UpdateWorkItemStatus 是工作表格的人工状态更新入口（v1：todo 三态
// pending/doing/done）。plan / subagent 状态由执行器权威管理，手动更新
// 返回明确错误；成功路径发布 runtime.changed（todo 快照）与 task/worktable
// 增量。
func (service *Service) UpdateWorkItemStatus(id, status string) error {
	kind, index, err := parseWorkItemID(id)
	if err != nil {
		return err
	}
	switch kind {
	case "todo":
		if err := service.Deps.Runtime.SetTodoStatus(index, dto.TodoItemStatus(strings.ToLower(strings.TrimSpace(status)))); err != nil {
			return err
		}
		service.refreshRuntimeAfterTodoChange()
		return nil
	default:
		return fmt.Errorf("work_item: %s 任务状态由执行器管理，不支持手动更新（仅支持 todo: 前缀）", kind)
	}
}

func parseWorkItemID(id string) (kind string, index int, err error) {
	parts := strings.SplitN(strings.TrimSpace(id), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, fmt.Errorf("work_item: 无效 ID %q（应为 todo:<index> 等）", id)
	}
	kind = parts[0]
	if kind == "todo" {
		index, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || index < 0 {
			return "", 0, fmt.Errorf("work_item: 无效 todo 索引 %q", parts[1])
		}
	}
	return kind, index, nil
}

// refreshRuntimeAfterTodoChange 在 todo 状态变更后重投影并发布三类增量：
// runtime.changed（既有面板/其他消费者）、worktable.changed 与 task.changed。
func (service *Service) refreshRuntimeAfterTodoChange() {
	projection := service.collectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	sessionID := service.Core.Snapshot.Session.ID
	items := CloneWorkItems(service.Core.Snapshot.Runtime.WorkTable)
	batches := CloneWorkTableBatches(service.Core.Snapshot.Runtime.WorkTableBatches)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, requestID, sessionID, service.Snapshot().Runtime)
	service.publishWorkTable(revision, requestID, items, batches)
}
