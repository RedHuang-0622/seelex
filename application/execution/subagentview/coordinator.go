// Package subagentview owns the subagent detail / tree projection
// surface: truncated session + context snapshot + worktree readback, and
// bounded tool-event increments into the authoritative Plan node snapshot. It
// only reads Engine Node query surfaces (safe read-only subagent actors) and
// never touches subagent execution.
package subagentview

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/event"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

// maxSubagentConversationMessages 单节点详情返回的会话消息上限。
const maxSubagentConversationMessages = 50

// maxSubagentContextItems 上下文快照单类条目上限。
const maxSubagentContextItems = 20

// Deps 是 subagentview 的装配输入。
type Deps struct {
	Core *state.Core
	// View 用于事件增量 revision bump（锁内调用）。
	View ViewPort
	// Limits 返回当前生效的运行时上限（截断配置）。
	Limits func() dto.ViewLimits
	// RecordToolOutcome 是工具记录的**叶子暂存入口**（装配处注入 core 的实现）。
	//
	// 口径（2026-10-19 方向 C，阶梯提交）：本包只报"这笔读数属于哪个执行单元
	// （owner 主会话 + 事件身份）、谁、什么状态、错误原文"；**单元键折算、成败判据
	// （toolStatusFailed + toolRecordReason）、本地暂存与主侧合并都在注入的实现里**
	// ——本包不复制判据、不写第二份词表。
	//
	// ownerSessionID 是**执行上下文**给出的归属主会话（Runtime 在节点注册时随 ctx
	// 绑定，装配层解析后交进来）。空 ⇒ 实现侧丢弃并计数，**本包不拿视图会话顶替**
	// （切会话串写就是这个病灶）。
	RecordToolOutcome func(ownerSessionID string, event dto.SubagentToolEvent)
	// CommitToolRecord 是**单元收口**的入口（节点进入终态时把暂存一次性交给主会话
	// 累计）：同步、返回即入账，不丢 goroutine。未注入 = 不收口（落盘前抽干会兜住）。
	CommitToolRecord func(ownerSessionID, nodeID string)
}

// ViewPort 是 Snapshot revision bump 的窄接口。
type ViewPort interface {
	BumpLocked() uint64
}

// Coordinator 拥有子代理详情/实时/树投影。
type Coordinator struct {
	*state.Core
	view              ViewPort
	limits            func() dto.ViewLimits
	recordToolOutcome func(ownerSessionID string, event dto.SubagentToolEvent)
	commitToolRecord  func(ownerSessionID, nodeID string)
}

// NewCoordinator 构造 subagentview 协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core: deps.Core, view: deps.View, limits: deps.Limits,
		recordToolOutcome: deps.RecordToolOutcome,
		commitToolRecord:  deps.CommitToolRecord,
	}
}

// HandleSubagentToolEvent 把 Runtime 工具分发投影进有界权威 Plan 节点快照
// 并发布一次前端增量。
//
// **兼容面**：这条入口没有 owner 归属（历史调用方只有事件）。读数因此不记
// （记账要求 owner 显式，见 HandleSubagentToolEventFor），Plan 投影照旧。
func (c *Coordinator) HandleSubagentToolEvent(e dto.SubagentToolEvent) {
	c.HandleSubagentToolEventFor("", e)
}

// HandleSubagentToolEventFor 是带归属的那条入口（生产路径）。
//
// 次序即语义（2026-10-19 方向 C）：
//  1. **记账先发生**——它不依赖计划节点，无 `NodeID` 时用 `subagent:<event.ID>` 作
//     单元键（旧实现把这类事件整条丢了，病灶 #9）；
//  2. 再做 Plan 投影，早退分支（无 NodeID/无 ID、无计划、节点不在树里）只表示
//     "这条消息不进投影"，不代表这次工具调用没发生过。
func (c *Coordinator) HandleSubagentToolEventFor(ownerSessionID string, e dto.SubagentToolEvent) {
	// ① 记账（叶子暂存的本地累加：不碰 ViewMu、不碰磁盘）。
	if c.recordToolOutcome != nil {
		c.recordToolOutcome(ownerSessionID, e)
	}
	if strings.TrimSpace(e.NodeID) == "" || strings.TrimSpace(e.ID) == "" {
		return // ② Plan 投影早退（记账已在上方完成）
	}
	// runtime 事件与快照投影同一类型（dto.SubagentToolEvent 单源）；此处只做
	// 有界截断，不复制字段。
	e.Arguments = c.truncateSubagentEvidence(e.Arguments)
	e.Result = c.truncateSubagentEvidence(e.Result)
	e.Error = c.truncateSubagentEvidence(e.Error)

	c.ViewMu.Lock()
	plan := c.Snapshot.Runtime.Plan
	if plan == nil {
		c.ViewMu.Unlock()
		return
	}
	node := FindPlanNodeByID(plan.Nodes, e.NodeID)
	if node == nil {
		c.ViewMu.Unlock()
		return
	}
	c.upsertSubagentToolEvent(node, e)
	revision := c.view.BumpLocked()
	requestID := c.Snapshot.Chat.RequestID
	// 事件路由键沿用快照会话（实时增量的收件人 = 当前视图）；**记账**的归属
	// 已经在上方按执行上下文定死了，两者互不替代（切会话不会再串写读数）。
	sessionID := c.Snapshot.Session.ID
	c.ViewMu.Unlock()

	kind := event.EventSubagentToolCompleted
	if e.Status == dto.ToolEventRunning {
		kind = event.EventSubagentToolStarted
	}
	if hub, ok := c.Events.(event.SessionAwareHub); ok {
		hub.PublishSession(kind, revision, requestID, sessionID, e)
	} else {
		c.Events.Publish(kind, revision, requestID, e)
	}
}

// CloseToolRecordUnit 是**节点收口**：把它这一轮的读数一次性提交给主会话累计
// （同步、返回即入账）。同一节点多次收口由实现侧幂等兜住（同 seq 只计一次）。
func (c *Coordinator) CloseToolRecordUnit(ownerSessionID, nodeID string) {
	if c.commitToolRecord == nil {
		return
	}
	c.commitToolRecord(ownerSessionID, nodeID)
}

func (c *Coordinator) upsertSubagentToolEvent(node *model.PlanNode, e model.SubagentToolEvent) {
	for index := range node.ToolEvents {
		if node.ToolEvents[index].ID == e.ID {
			node.ToolEvents[index] = e
			return
		}
	}
	node.ToolEvents = append(node.ToolEvents, e)
	if limit := c.limits().PlanNodeEvents; limit > 0 && len(node.ToolEvents) > limit {
		node.ToolEvents = append([]model.SubagentToolEvent(nil), node.ToolEvents[len(node.ToolEvents)-limit:]...)
	}
}

func (c *Coordinator) truncateSubagentEvidence(value string) string {
	limit := c.limits().EvidenceChars
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

// FindPlanNodeByID 递归查找 Plan 节点（含子节点）。
func FindPlanNodeByID(nodes []model.PlanNode, nodeID string) *model.PlanNode {
	for index := range nodes {
		if nodes[index].ID == nodeID {
			return &nodes[index]
		}
		if node := FindPlanNodeByID(nodes[index].Children, nodeID); node != nil {
			return node
		}
	}
	return nil
}

// ClonePlanNode 深拷贝单个 Plan 节点（事件投影发布用）。
func ClonePlanNode(node model.PlanNode) model.PlanNode {
	cloned := model.CloneRuntimeState(model.RuntimeState{Plan: &model.PlanState{Nodes: []model.PlanNode{node}}})
	return cloned.Plan.Nodes[0]
}

// SubagentChangedPayload 组装子代理变更事件负载。
func SubagentChangedPayload(plan *model.PlanState, planID, runID string, node model.PlanNode) model.SubagentEvent {
	return model.SubagentEvent{
		PlanID: planID, RunID: runID, NodeID: node.ID, Node: ClonePlanNode(node),
		PlanStatus: plan.Status, Progress: plan.Progress,
	}
}

// SubagentDetail 返回节点子代理详情（截断会话 + 上下文快照 + 功能打点 +
// worktree 现场；只读子代理 actor，安全）。归属/Goal/SessionID 来自
// SubAgentTree 或工作台行（fork 不在 Plan 快照也可展示）；Trace 来自工作台
// 打点。这些数据面供 GUI 打开详情后节流刷新（弹窗只剩会话记录/上下文/
// 功能打点三块）。
func (c *Coordinator) SubagentDetail(nodeID string) (*model.SubagentDetail, error) {
	if nodeID == "" {
		return nil, fmt.Errorf("subagent detail: node id is required")
	}
	// 锁序修复（2026-09-08 死锁复现）：引擎 SubAgentTree() 内部会读节点
	// Session.History（Session.mu），而节点执行 goroutine 持 Session.mu
	// 等待 ViewMu.Lock（HandleSubagentToolEvent）。若在此处先取 ViewMu.RLock
	// 再调引擎，会形成 ViewMu → Session.mu 与 Session.mu → ViewMu 的循环
	// 等待。因此子树查找必须在 ViewMu 锁外完成（只读引擎 actor，安全）。
	var treeNode *dto.SubAgentTreeNode
	if c.Deps.Engine != nil {
		if found := findSubagentTreeNode(c.Deps.Engine.SubAgentTree(), nodeID); found != nil {
			treeNode = found
		}
	}
	c.ViewMu.RLock()
	var status model.NodeStatus
	var elapsed string
	var toolEvents []model.SubagentToolEvent
	plan := c.Snapshot.Runtime.Plan
	if plan != nil {
		if node := FindPlanNodeByID(plan.Nodes, nodeID); node != nil {
			status = node.Status
			elapsed = node.Elapsed
			toolEvents = append([]model.SubagentToolEvent(nil), node.ToolEvents...)
		}
	}
	// 工作台行（kind=subagent，key=subagent:<nodeID>）：回填 Assignee/
	// Participants/打点与任务状态兜底。
	var workRow *model.WorkItem
	for index := range c.Snapshot.Runtime.WorkTable {
		item := &c.Snapshot.Runtime.WorkTable[index]
		if item.Kind == "subagent" && item.SourceID == nodeID {
			workRow = item
			break
		}
	}
	if workRow == nil {
		for index := range c.Snapshot.Runtime.WorkTable {
			item := &c.Snapshot.Runtime.WorkTable[index]
			if item.ID == "subagent:"+nodeID {
				workRow = item
				break
			}
		}
	}
	if status == model.NodeUnknown && workRow != nil {
		if mapped := nodeStatusFromTaskStatus(workRow.Status); mapped != model.NodeUnknown {
			status = mapped
		}
	}
	if status == model.NodeUnknown && treeNode != nil {
		if mapped := nodeStatusFromSubagentStatus(treeNode.Status); mapped != model.NodeUnknown {
			status = mapped
		}
	}
	var goal, sessionID, summary string
	if treeNode != nil {
		goal = treeNode.Goal
		sessionID = treeNode.SessionID
		summary = strings.TrimSpace(treeNode.Summary)
		if summary == "" {
			summary = strings.TrimSpace(treeNode.Error)
		}
	}
	var nodeError string
	if treeNode != nil {
		nodeError = strings.TrimSpace(treeNode.Error)
	}
	c.ViewMu.RUnlock()

	conversation, ok := c.Deps.Engine.NodeSessionConversation(nodeID)
	if !ok && status == model.NodeUnknown && treeNode == nil && workRow == nil {
		return nil, fmt.Errorf("subagent detail: node %q has no conversation", nodeID)
	}
	contextSnap, _ := c.Deps.Engine.NodeContextSnapshot(nodeID)
	var assignee string
	var participants []string
	var trace []model.WorkTracePoint
	if workRow != nil {
		assignee = workRow.Assignee
		participants = append([]string(nil), workRow.Participants...)
		trace = append([]model.WorkTracePoint(nil), workRow.Trace...)
		if limit := c.limits().PlanNodeEvents; limit > 0 && len(trace) > limit {
			trace = trace[len(trace)-limit:]
		}
	}
	// Plan 快照失败节点可能只在 worktable trace 带 evidence：兜底取最后
	// 一条失败打点，保证详情错误面不空。
	if nodeError == "" && status == model.NodeFailed {
		for index := len(trace) - 1; index >= 0; index-- {
			lower := strings.ToLower(trace[index].Status + " " + trace[index].Operation)
			if strings.Contains(lower, "fail") && strings.TrimSpace(trace[index].Evidence) != "" {
				nodeError = strings.TrimSpace(trace[index].Evidence)
				break
			}
		}
	}
	detail := &model.SubagentDetail{
		Running:      isRunningSubagentStatus(status),
		Status:       status,
		Elapsed:      elapsed,
		Goal:         goal,
		SessionID:    sessionID,
		Assignee:     assignee,
		Participants: participants,
		Summary:      summary,
		Error:        nodeError,
		Conversation: c.adaptSubagentConversation(conversation),
		ToolEvents:   toolEvents,
		Context:      c.adaptSubagentContext(contextSnap),
		Worktree:     c.nodeWorktreeInfo(nodeID),
		Trace:        trace,
	}
	return detail, nil
}

// nodeStatusFromSubagentStatus 把子代理树状态映射为详情状态。
func nodeStatusFromSubagentStatus(status dto.SubAgentNodeStatus) model.NodeStatus {
	switch status {
	case dto.SubAgentQueued:
		return model.NodeQueued
	case dto.SubAgentRunning:
		return model.NodeRunning
	case dto.SubAgentDone:
		return model.NodeCompleted
	case dto.SubAgentFailed, dto.SubAgentInterrupted:
		return model.NodeFailed
	default:
		return model.NodeUnknown
	}
}

// nodeStatusFromTaskStatus 把任务注册表状态映射为详情状态（未知 → NodeUnknown）。
//
// 边界映射：subject 是**工作表格行**的状态字段——那是混合值（task 行的词来自 dto.Task*，
// 子代理行沿用记录词的显示映射 done/failed，见 application/core/work_table.go）。所以这里
// 是**读 wire 词的边界**，但词不再写第二份字面量：一律从契约枚举取对外词。
func nodeStatusFromTaskStatus(status string) model.NodeStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case dto.TaskPending.String():
		return model.NodePending
	case dto.TaskQueued.String():
		return model.NodeQueued
	case dto.TaskRunning.String(), dto.TaskDoing.String():
		return model.NodeRunning
	case dto.TaskCompleted.String(), dto.SubAgentDone.String():
		return model.NodeCompleted
	case dto.TaskFailed.String(), dto.TaskInterrupted.String():
		return model.NodeFailed
	default:
		return model.NodeUnknown
	}
}

// findSubagentTreeNode 在子代理树投影中递归查找节点。
func findSubagentTreeNode(nodes []dto.SubAgentTreeNode, nodeID string) *dto.SubAgentTreeNode {
	for index := range nodes {
		node := &nodes[index]
		if node.ID == nodeID {
			return node
		}
		if found := findSubagentTreeNode(node.Children, nodeID); found != nil {
			return found
		}
	}
	return nil
}

func (c *Coordinator) nodeWorktreeInfo(nodeID string) *model.SubagentWorktreeInfo {
	if c.Deps.Engine == nil {
		return nil
	}
	info, ok := c.Deps.Engine.NodeWorktreeInfoFor(nodeID)
	if !ok || info.Path == "" {
		return nil
	}
	return &model.SubagentWorktreeInfo{
		Path: info.Path, Branch: info.Branch, MainBranch: info.MainBranch,
	}
}

func (c *Coordinator) adaptSubagentContext(snap *dto.ContextSnapshot) *model.SubagentContext {
	if snap == nil {
		return nil
	}
	limit := c.limits().EvidenceChars
	truncate := func(value string) string {
		if limit > 0 && len(value) > limit {
			return value[:limit] + "…"
		}
		return value
	}
	context := &model.SubagentContext{
		Goal:          truncate(snap.Goal),
		Progress:      truncate(snap.Progress),
		MessageCount:  snap.MessageCount,
		TokenEstimate: snap.TokenEstimate,
	}
	for _, finding := range snap.Findings {
		if len(context.Findings) >= maxSubagentContextItems {
			break
		}
		if finding = truncate(finding); finding != "" {
			context.Findings = append(context.Findings, finding)
		}
	}
	for _, decision := range snap.Decisions {
		if len(context.Decisions) >= maxSubagentContextItems {
			break
		}
		context.Decisions = append(context.Decisions, model.SubagentContextDecision{
			What: truncate(decision.What), Why: truncate(decision.Why),
		})
	}
	for _, constraint := range snap.Constraints {
		if len(context.Constraints) >= maxSubagentContextItems {
			break
		}
		if constraint = truncate(constraint); constraint != "" {
			context.Constraints = append(context.Constraints, constraint)
		}
	}
	for _, work := range snap.PendingWork {
		if len(context.PendingWork) >= maxSubagentContextItems {
			break
		}
		if work = truncate(work); work != "" {
			context.PendingWork = append(context.PendingWork, work)
		}
	}
	return context
}

func isRunningSubagentStatus(status model.NodeStatus) bool {
	switch status {
	case model.NodeRunning, model.NodeWorktreeCreating, model.NodeRebasing, model.NodeMerging:
		return true
	default:
		return false
	}
}

func (c *Coordinator) adaptSubagentConversation(messages []contract.EngineMessage) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	limit := c.limits().EvidenceChars
	adapted := make([]model.Message, 0, min(len(messages), maxSubagentConversationMessages))
	for _, msg := range messages {
		if len(adapted) >= maxSubagentConversationMessages {
			break
		}
		content := msg.Content
		if limit > 0 && len(content) > limit {
			content = content[:limit] + "…"
		}
		message := model.Message{Role: msg.Role, Content: content}
		if msg.Name != "" || msg.ToolCallID != "" {
			message.Tool = &model.ToolCall{
				ID: msg.ToolCallID, Name: msg.Name, Status: dto.ToolEventSuccess,
			}
		}
		adapted = append(adapted, message)
	}
	return adapted
}
