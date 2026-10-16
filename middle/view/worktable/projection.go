package worktable

// projection.go — 工作表格**投影判据**：把权威状态（task 注册表快照、plan 邻接面、
// 子代理树、后台执行登记表）投影成目录行 / 批次头 / 行内打点。
//
// 为什么判据在这里而不是宿主（`application/core`）：行怎么排、状态怎么映射、证据怎么
// 截断，都是"从权威快照派生"的纯计算；宿主只负责在它自己的锁下取数。判据留在宿主会
// 让同一份派生规则随用例膨胀，也让中间层无法独立复用投影。
//
// 与宿主的边界（M5-R5b）：本文件**不自取锁、不读宿主字段、不 import `backend/limit`**——
// 锁、进程级上限与 Runtime 取数都归宿主，输入按参数传入（见 Limits）。

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// Limits 是工作表格投影用到的**进程级上限**三格：宿主从 `backend/limit` 取好按值传入
// （中间层不反向依赖它，见 plan §1.2 与 R5c 的偏差登记）。
//
// 为什么是三格而不是整份 `limit.Limits`：投影只用得到这三格，收窄参数面让"投影依赖了
// 哪些上限"一眼可读；宿主侧的唯一转换点在 `application/core/limits.go`。
type Limits struct {
	// WorkTableRows 是表格行数上限（0 = 不限）。
	WorkTableRows int
	// EvidenceChars 是证据/描述文本的字符上限（0 = 不限）。
	EvidenceChars int
	// PlanNodeEvents 是单行打点的事件上限（0 = 不限）。
	PlanNodeEvents int
}

// BuildWorkTable 组装工作表格行：注册表 task → model.WorkItem；plan 行额外合并
// 节点事件/工具活动打点（详情数据面仍直接读 plan 节点）；后台命令（asyncRuns）
// 以**只读投影**并入，不进注册表（见 work_table_async.go 与不变量 I-21）。
// 有界：行数 ≤ limits.work_table_rows，trace ≤ WorkTableTraceLimit。
func BuildWorkTable(plan *model.PlanState, tasks []dto.TaskRecord, subagentTree []dto.SubAgentTreeNode, asyncRuns []dto.AsyncRunRecord, limits Limits) []model.WorkItem {
	nodeByID := make(map[string]model.PlanNode)
	if plan != nil {
		var walk func(nodes []model.PlanNode)
		walk = func(nodes []model.PlanNode) {
			for _, node := range nodes {
				nodeByID[node.ID] = node
				walk(node.Children)
			}
		}
		walk(plan.Nodes)
	}
	phaseOrder := map[string]int{"plan": 0, "task": 1, "tasklist": 2, "subagent": 3}
	rows := make([]model.WorkItem, 0, len(tasks))
	for _, record := range tasks {
		item := TaskRecordToWorkItem(record, limits)
		if record.Kind == "plan" {
			if node, ok := nodeByID[PlanNodeIDFor(record)]; ok {
				item.Trace = BoundWorkTrace(append(item.Trace, PlanNodeTrace(node, plan != nil && plan.Status != model.PlanRunning, limits)...), limits)
				// plan 行依赖取自 plan 邻接面（plan_load 产出的是平铺节点表 +
				// 边集，注册表按树形父子推导会让 plan 行的「依赖」列恒为空）。
				item.Dependencies = MergeWorkDependencies(item.Dependencies, PlanDependencies(plan, node.ID))
			}
		}
		rows = append(rows, item)
	}
	rows = append(rows, AsyncWorkItems(asyncRuns, limits)...)
	sort.SliceStable(rows, func(left, right int) bool {
		leftOrder := phaseOrder[rows[left].Phase]
		rightOrder := phaseOrder[rows[right].Phase]
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return rows[left].ID < rows[right].ID
	})
	if limit := limits.WorkTableRows; limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// TaskRecordToWorkItem 把注册表 task 快照映射为 model.WorkItem（含 retry 计数）。
func TaskRecordToWorkItem(record dto.TaskRecord, limits Limits) model.WorkItem {
	trace := make([]model.WorkTracePoint, 0, len(record.Trace))
	for _, point := range record.Trace {
		trace = append(trace, model.WorkTracePoint{
			At: point.At, Status: point.Status.String(), Operation: point.Operation,
			Evidence: TruncateWorkEvidence(point.Evidence, limits.EvidenceChars), Duration: point.Duration,
		})
	}
	trace = BoundWorkTrace(trace, limits)
	status := record.Status.String()
	if record.Kind == "todo" && record.Status == dto.TaskCompleted {
		// todo 三态契约：done（前端状态按钮 active 判定兼容）。
		status = "done"
	}
	if record.Kind == "subagent" && record.Status == dto.TaskCompleted {
		// 子代理阶段沿用 running/done/failed 语义。
		status = "done"
	}
	return model.WorkItem{
		ID: record.ID, SessionID: record.SessionID, Phase: record.Phase,
		Task:        TruncateWorkEvidence(record.Task, 200),
		Description: TruncateWorkEvidence(record.Description, limits.EvidenceChars),
		Status:      status, RetryCount: record.RetryCount, Assignee: record.Assignee,
		Dependencies: append([]string(nil), record.Dependencies...),
		Attachments:  append([]string(nil), record.Attachments...),
		Kind:         record.Kind, SourceID: record.SourceID,
		Participants: append([]string(nil), record.Participants...),
		BatchID:      record.BatchID,
		BatchLabel:   BatchLabel(record.BatchID, record.CreatedAt),
		CreatedAt:    record.CreatedAt,
		StartedAt:    record.StartedAt, EndedAt: record.EndedAt, Elapsed: record.Elapsed,
		Trace: trace,
	}
}

// LegacyBatchID 是早期/未分批会话的伪批次 ID（空串；展示标签「早期任务」，
// 排序置底，不与真实 chat 请求批次混排）。
const LegacyBatchID = ""

// BatchLabel 由批次 ID 与创建时间派生展示标签：真实批次用本地时间
// （YYYY-MM-DD HH:MM），早期/未分批会话固定为「早期任务」。
func BatchLabel(id string, createdAt time.Time) string {
	if id == "" {
		return "早期任务"
	}
	if createdAt.IsZero() {
		return id
	}
	return createdAt.Format("2006-01-02 15:04")
}

// BuildWorkTableBatches 从工作表格行派生批次分片头：按 BatchID 分组，
// 组内取最早 CreatedAt 作为批次时间；按 CreatedAt 升序返回，空批次（早期
// 会话）置底。Counts 按权威类型统计（all/plan/task/todo/subagent），
// 仅包含有内容的批次。
func BuildWorkTableBatches(rows []model.WorkItem) []model.WorkTableBatch {
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
	batches := make([]model.WorkTableBatch, 0, len(order))
	for _, id := range order {
		group := byID[id]
		batches = append(batches, model.WorkTableBatch{
			ID:        group.id,
			Label:     BatchLabel(group.id, group.createdAt),
			CreatedAt: group.createdAt,
			Counts:    group.counts,
		})
	}
	sort.SliceStable(batches, func(left, right int) bool {
		leftLegacy := batches[left].ID == LegacyBatchID
		rightLegacy := batches[right].ID == LegacyBatchID
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

// PlanNodeTrace 由节点事件 + 子代理工具活动合成打点（按时间倒序、有界；
// 详情弹窗数据面仍直接读 plan 节点，此处只是工作表格概览合并）。
func PlanNodeTrace(node model.PlanNode, tasklistMode bool, limits Limits) []model.WorkTracePoint {
	points := make([]model.WorkTracePoint, 0, len(node.Events)+len(node.ToolEvents))
	for _, event := range node.Events {
		operation := "node.lifecycle"
		if tasklistMode && event.Status == model.NodeCompleted.String() {
			operation = "task_check_node"
		}
		points = append(points, model.WorkTracePoint{
			At: event.At, Status: event.Status, Operation: operation,
			Evidence: TruncateWorkEvidence(event.Output, limits.EvidenceChars),
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
		points = append(points, model.WorkTracePoint{
			At: tool.StartedAt, Status: tool.Status.String(), Operation: tool.Name,
			Evidence: TruncateWorkEvidence(evidence, limits.EvidenceChars),
			Duration: FormatWorkDuration(tool.Duration),
		})
	}
	return points
}

// WorkTableTraceLimit 是工作表格概览的行内打点上限（完整时间线在详情弹窗）。
const WorkTableTraceLimit = 10

// BoundWorkTrace 按时间倒序排序并截断。
func BoundWorkTrace(points []model.WorkTracePoint, limits Limits) []model.WorkTracePoint {
	sort.SliceStable(points, func(left, right int) bool {
		return points[left].At.After(points[right].At)
	})
	if limit := limits.PlanNodeEvents; limit > 0 && len(points) > limit {
		points = points[:limit]
	}
	if len(points) > WorkTableTraceLimit {
		points = points[:WorkTableTraceLimit]
	}
	return points
}

func FormatWorkDuration(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	ms := float64(duration) / float64(time.Millisecond)
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// SubagentTreePayloadSignature 生成子代理树投影的内容签名，用于判断
// worktable.changed 是否需要随包携带树。签名覆盖身份/状态/会话号与各文本
// 字段、上下文计数的长度，既能识别投影内容变化，也避免为比较把整棵树
// 序列化一遍（树是运行期唯一较重的投影，表格增量本身要保持轻量）。
func SubagentTreePayloadSignature(nodes []dto.SubAgentTreeNode) string {
	var builder strings.Builder
	var walk func(items []dto.SubAgentTreeNode)
	walk = func(items []dto.SubAgentTreeNode) {
		fmt.Fprintf(&builder, "%d:", len(items))
		for _, node := range items {
			fmt.Fprintf(&builder, "%s|%s|%s|%s|%d|%d|%d|%d|%d;",
				node.ID, node.Status, node.SessionID, node.MainSessionID,
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

func TaskStatusForNode(status model.NodeStatus) dto.TaskStatus {
	switch status {
	case model.NodeRunning, model.NodeWorktreeCreating, model.NodeRebasing, model.NodeMerging, model.NodeQueued:
		return dto.TaskRunning
	case model.NodeCompleted, model.NodeSkipped:
		return dto.TaskCompleted
	case model.NodeFailed, model.NodePanicked:
		return dto.TaskFailed
	case model.NodeAborted, model.NodeCanceled:
		return dto.TaskFailed
	default:
		return dto.TaskPending
	}
}

func TaskStatusForSubagent(status dto.SubAgentNodeStatus) dto.TaskStatus {
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

// PlanNodeIDFor 返回 plan 行对应的 plan 节点 ID：SourceID 优先，缺失时
// 回退 ID 的 plan: 前缀（恢复会话/老数据可能没有 SourceID，而 plan:<nodeID>
// 是工作表格行的权威键）。
func PlanNodeIDFor(record dto.TaskRecord) string {
	if record.SourceID != "" {
		return record.SourceID
	}
	if nodeID, ok := strings.CutPrefix(record.ID, "plan:"); ok {
		return nodeID
	}
	return ""
}

// PlanDependencies 由 plan 邻接面取节点的前置任务 ID（plan:<前置节点>）。
//
// plan 的依赖是 **DAG 入边**（plan_load 的 edges），不是树形父子关系：
// plan_load 产出的是平铺节点表 + 边集，按父子推导会让所有 plan 行的依赖
// 恒为空——工作表格「依赖」列对 plan 行不可见（用户看不到自己在等谁）。
func PlanDependencies(plan *model.PlanState, nodeID string) []string {
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

// MergeWorkDependencies 合并依赖来源（注册表记录 + plan 邻接面）：去重、
// 稳定排序（UI 与用例的确定性），空结果返回 nil（JSON omitempty 面）。
func MergeWorkDependencies(recorded []string, derived []string) []string {
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

// MergeTaskRecords 合并两组 task 记录（按跨会话身份去重，前者优先，稳定
// 排序）：冷读面用全局表 + 会话持久化的自身条目拼一张表，保证"任何入口看到
// 的都是同一条跨会话台账"。身份 = 幂等键优先、否则行 ID：同一 plan 节点/
// 子代理/待办文本在不同来源（实时注册表、会话分区、磁盘记录）出现时靠 Key
// 认同一行；自动行 ID（`task:<n>`/`todo:<n>`）由进程级分配器保证进程内唯一，
// 不会把两个会话的条目并成一行（见 backend/tool/task.NextAutoID）。
//
// secondarySessionID 标注 secondary 的归属会话：磁盘记录不带会话标记（归属
// 由容器 `SessionRecord` 表达），冷读时把该会话的持久化条目补上标记，会话
// 筛选轴才不会把它们的行判成"无主"。
//
// **身份判据由宿主注入**（M5-R5b 的依赖倒置，AGENTS §8.3）：判据的唯一实现在
// `backend/engine/task.LedgerIdentity`（台账注册表读面也用它），本层不复制一份
// ——中间层不能 import 后端域包，故走参数（与 R6-input-index 的
// `BuildInputIndex(..., userText)` 同一招）。注入函数返回空串 = 该行没有身份，
// 不进合并（宿主判据不会返回空串，这里是防御性分支）。
func MergeTaskRecords(primary, secondary []dto.TaskRecord, secondarySessionID string, identity func(dto.TaskRecord) string) []dto.TaskRecord {
	if len(secondary) == 0 {
		return primary
	}
	merged := make([]dto.TaskRecord, 0, len(primary)+len(secondary))
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	appendRecord := func(record dto.TaskRecord) {
		key := identity(record)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
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

func ClonePlanForSync(plan *model.PlanState) *model.PlanState {
	if plan == nil {
		return nil
	}
	return model.CloneRuntimeState(model.RuntimeState{Plan: plan}).Plan
}

func CloneSubAgentTreeForSync(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]dto.SubAgentTreeNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = CloneSubAgentTreeForSync(nodes[index].Children)
	}
	return cloned
}
