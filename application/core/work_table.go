package core

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge"
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
// 节点事件/工具活动打点（详情数据面仍直接读 plan 节点）。有界：行数 ≤
// limits.work_table_rows，trace ≤ workTableTraceLimit。
func buildWorkTable(plan *PlanState, tasks []seelebridge.TaskRecord, subagentTree []seelebridge.SubAgentTreeNode) []WorkItem {
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
			if node, ok := nodeByID[record.SourceID]; ok {
				item.Trace = boundWorkTrace(append(item.Trace, planNodeTrace(node, plan != nil && plan.Status != PlanRunning)...))
			}
		}
		rows = append(rows, item)
	}
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
func taskRecordToWorkItem(record seelebridge.TaskRecord) WorkItem {
	trace := make([]WorkTracePoint, 0, len(record.Trace))
	for _, point := range record.Trace {
		trace = append(trace, WorkTracePoint{
			At: point.At, Status: point.Status, Operation: point.Operation,
			Evidence: truncateWorkEvidence(point.Evidence, Limits().EvidenceChars), Duration: point.Duration,
		})
	}
	trace = boundWorkTrace(trace)
	status := string(record.Status)
	if record.Kind == "todo" && record.Status == seelebridge.TaskCompleted {
		// todo 三态契约：done（前端状态按钮 active 判定兼容）。
		status = "done"
	}
	if record.Kind == "subagent" && record.Status == seelebridge.TaskCompleted {
		// 子代理阶段沿用 running/done/failed 语义。
		status = "done"
	}
	return WorkItem{
		ID: record.ID, Phase: record.Phase,
		Task:        truncateWorkEvidence(record.Task, 200),
		Description: truncateWorkEvidence(record.Description, Limits().EvidenceChars),
		Status:      status, RetryCount: record.RetryCount, Assignee: record.Assignee,
		Dependencies: append([]string(nil), record.Dependencies...),
		Attachments:  append([]string(nil), record.Attachments...),
		Kind:         record.Kind, SourceID: record.SourceID,
		Participants: append([]string(nil), record.Participants...),
		StartedAt:    record.StartedAt, EndedAt: record.EndedAt, Elapsed: record.Elapsed,
		Trace: trace,
	}
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

// refreshWorkTableLocked 在 service.mu 持锁时重建工作表格投影。
func (state *serviceState) refreshWorkTableLocked(tasks []seelebridge.TaskRecord) {
	state.snapshot.Runtime.WorkTable = buildWorkTable(
		state.snapshot.Runtime.Plan,
		tasks,
		state.snapshot.Runtime.SubAgentTree,
	)
}

// publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
// 是同一临界区克隆的不可变快照，保证 revision 与内容一致）。
func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []WorkItem) {
	if state.workTablePublisher == nil {
		return
	}
	state.workTablePublisher.Send(worktableUpdate{revision: revision, requestID: requestID, items: items})
}

// publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
// payload 小，逐任务保证不丢）。
func (state *serviceState) publishTaskChanged(record seelebridge.TaskRecord, revision uint64, requestID string) {
	if state.events == nil {
		return
	}
	item := taskRecordToWorkItem(record)
	state.events.Publish(EventTaskChanged, revision, requestID, TaskChangedEvent{TaskID: item.ID, Task: item})
}

// publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
// worktable.changed（结构安全网）；task.changed 由 CSP 消费者直发。
func (service *Service) publishTaskDeltas() {
	tasks := service.deps.Runtime.TaskSnapshot()
	service.mu.Lock()
	service.refreshWorkTableLocked(tasks)
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	items := CloneWorkItems(service.snapshot.Runtime.WorkTable)
	service.mu.Unlock()
	service.publishWorkTable(revision, requestID, items)
}

// syncTasksFromSources 把 plan 节点与子代理树的生命周期投影进 task 注册表
// （被动触发；锁外调用外部端口，状态不一致才写入，避免 trace 刷屏）。
func (service *Service) syncTasksFromSources() {
	service.mu.RLock()
	plan := clonePlanForSync(service.snapshot.Runtime.Plan)
	tree := cloneSubAgentTreeForSync(service.snapshot.Runtime.SubAgentTree)
	service.mu.RUnlock()

	if plan != nil {
		var walk func(nodes []PlanNode, parentID string)
		walk = func(nodes []PlanNode, parentID string) {
			for _, node := range nodes {
				service.syncPlanNodeTask(node, parentID)
				walk(node.Children, "plan:"+node.ID)
			}
		}
		walk(plan.Nodes, "")
	}
	var walkTree func(items []seelebridge.SubAgentTreeNode, parentID string)
	walkTree = func(items []seelebridge.SubAgentTreeNode, parentID string) {
		for _, node := range items {
			if node.ID == "" || node.ID == "main" {
				walkTree(node.Children, parentID)
				continue
			}
			service.syncSubagentTask(node, parentID)
			walkTree(node.Children, node.ID)
		}
	}
	walkTree(tree, "")
}

func (service *Service) syncPlanNodeTask(node PlanNode, parentID string) {
	key := "plan:" + node.ID
	status := taskStatusForNode(node.Status)
	existing, found, err := service.deps.Runtime.ResolveTaskByKey(key)
	if err != nil {
		return
	}
	if !found {
		spec := seelebridge.TaskSpec{
			ID: key, Key: key, Phase: seelebridge.TaskPhasePlan, Task: node.Label, Kind: "plan",
			SourceID: node.ID,
		}
		if parentID != "" {
			spec.Dependencies = []string{parentID}
		}
		_, _, _ = service.deps.Runtime.TaskAdd(spec)
		return
	}
	if existing.Status != status {
		_, _ = service.deps.Runtime.TaskSetStatus(existing.ID, status, "node:"+string(node.Status))
	}
}

func (service *Service) syncSubagentTask(node seelebridge.SubAgentTreeNode, parentID string) {
	key := "subagent:" + node.ID
	status := taskStatusForSubagent(node.Status)
	existing, found, err := service.deps.Runtime.ResolveTaskByKey(key)
	if err != nil {
		return
	}
	if !found {
		spec := seelebridge.TaskSpec{
			ID: key, Key: key, Phase: seelebridge.TaskPhaseSubagent, Task: node.Goal, Kind: "subagent",
			Assignee: node.ID, SourceID: node.ID,
		}
		if parentID != "" {
			spec.Dependencies = []string{"subagent:" + parentID}
		}
		created, _, _ := service.deps.Runtime.TaskAdd(spec)
		_, _ = service.deps.Runtime.TaskSetStatus(created.ID, status, "subagent:"+string(node.Status))
		_, _ = service.deps.Runtime.TaskAttachParticipant(created.ID, node.ID)
		return
	}
	if existing.Status != status {
		_, _ = service.deps.Runtime.TaskSetStatus(existing.ID, status, "subagent:"+string(node.Status))
	}
	_, _ = service.deps.Runtime.TaskAttachParticipant(existing.ID, node.ID)
}

func taskStatusForNode(status NodeStatus) seelebridge.TaskStatus {
	switch status {
	case NodeRunning, NodeWorktreeCreating, NodeRebasing, NodeMerging, NodeQueued:
		return seelebridge.TaskRunning
	case NodeCompleted, NodeSkipped:
		return seelebridge.TaskCompleted
	case NodeFailed, NodePanicked:
		return seelebridge.TaskFailed
	case NodeAborted, NodeCanceled:
		return seelebridge.TaskFailed
	default:
		return seelebridge.TaskPending
	}
}

func taskStatusForSubagent(status seelebridge.SubAgentNodeStatus) seelebridge.TaskStatus {
	switch status {
	case seelebridge.SubAgentQueued:
		return seelebridge.TaskQueued
	case seelebridge.SubAgentDone:
		return seelebridge.TaskCompleted
	case seelebridge.SubAgentFailed:
		return seelebridge.TaskFailed
	default:
		return seelebridge.TaskRunning
	}
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

// workTableTraceBlock 构建打点表标记块：只含未终态任务
// （pending/running/doing/retry），按 id 稳定排序；无活动任务返回空串
// （块随任务完成自动删除）。
func (state *serviceState) workTableTraceBlock() string {
	if state == nil || state.deps.Runtime == nil {
		return ""
	}
	records := state.deps.Runtime.TaskSnapshot()
	active := make([]seelebridge.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Status != seelebridge.TaskCompleted && record.Status != seelebridge.TaskFailed {
			active = append(active, record)
		}
	}
	if len(active) == 0 {
		return ""
	}
	sort.SliceStable(active, func(left, right int) bool {
		return active[left].ID < active[right].ID
	})
	var builder strings.Builder
	builder.WriteString(workTableTraceMarkerOpen + "\n# 工作打点表（系统维护，只读；任务状态与打点以工作表格为准）\n")
	lines := 2
	for _, record := range active {
		if lines >= workTableTraceMaxLines {
			builder.WriteString("- …（打点表已达上限，详情见工作表格）\n")
			break
		}
		retry := ""
		if record.RetryCount > 0 {
			retry = fmt.Sprintf(" retry=%d", record.RetryCount)
		}
		builder.WriteString(fmt.Sprintf("- %s %s%s %s\n",
			record.ID, record.Status, retry, truncateWorkEvidence(record.Task, 80)))
		lines++
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

func cloneSubAgentTreeForSync(nodes []seelebridge.SubAgentTreeNode) []seelebridge.SubAgentTreeNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]seelebridge.SubAgentTreeNode(nil), nodes...)
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
	// （拿子代理会话锁），不能在持 service.mu 时调用——避免与 runner 持会话
	// 锁 → 注册表 actor → 变更 channel 的路径成环死锁。
	tree := service.deps.Engine.SubAgentTree()
	service.mu.Lock()
	service.snapshot.Runtime.SubAgentTree = tree
	service.mu.Unlock()
	service.refreshWorkTableFromSources()
}

// refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
// 注册表，再发布 worktable.changed + task.changed。
func (service *Service) refreshWorkTableFromSources() {
	service.syncTasksFromSources()
	service.publishTaskDeltas()
}

// ── CSP 生命周期消费者（取代同步回调 observer）────────────

// startLifecycleConsumers 启动三个消费者 goroutine：子代理树信号 → 刷新
// 工作表格；plan 节点事件 → 投影；task 变更 → 直发 task.changed。数据经
// channel（CSP）流转，runtime 侧不再同步回调进 application。
func (service *Service) startLifecycleConsumers() {
	if service.deps.Runtime == nil {
		return
	}
	if service.lifecycleStop == nil {
		service.lifecycleStop = make(chan struct{})
	}
	go service.consumeSubagentLifecycle()
	go service.consumePlanNodeEvents()
	go service.consumeTaskChanges()
}

func (service *Service) stopLifecycleConsumers() {
	if service.lifecycleStop != nil {
		service.lifecycleOnce.Do(func() { close(service.lifecycleStop) })
	}
}

func (service *Service) consumeSubagentLifecycle() {
	events := service.deps.Runtime.SubagentTreeEvents()
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
	events := service.deps.Runtime.PlanNodeEventChannel()
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
	changes := service.deps.Runtime.TaskChangedChannel()
	if changes == nil {
		return
	}
	for {
		select {
		case record, ok := <-changes:
			if !ok {
				return
			}
			service.mu.RLock()
			revision := service.snapshot.Revision
			requestID := service.snapshot.Chat.RequestID
			service.mu.RUnlock()
			service.safeLifecycleCall(func() { service.publishTaskChanged(record, revision, requestID) })
		case <-service.lifecycleStop:
			return
		}
	}
}

// safeLifecycleCall 隔离消费者中的单次 panic（记录并继续，避免消费者
// goroutine 静默死亡导致工作表格停止刷新）。
func (service *Service) safeLifecycleCall(call func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("seelex lifecycle consumer recovered: %v", recovered)
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
		if err := service.deps.Runtime.SetTodoStatus(index, seelebridge.TodoItemStatus(strings.ToLower(strings.TrimSpace(status)))); err != nil {
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
	service.mu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	items := CloneWorkItems(service.snapshot.Runtime.WorkTable)
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, requestID, service.Snapshot().Runtime)
	service.publishWorkTable(revision, requestID, items)
}
