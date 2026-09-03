// Package subagent_view owns the subagent detail / live / tree projection
// surface: truncated session + context snapshot + worktree readback, live
// subscription passthrough, and bounded tool-event increments into the
// authoritative Plan node snapshot. It only reads Engine Node query surfaces
// (safe read-only subagent actors) and never touches subagent execution.
package subagent_view

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	seelsession "github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// maxSubagentConversationMessages 单节点详情返回的会话消息上限。
const maxSubagentConversationMessages = 50

// maxSubagentContextItems 上下文快照单类条目上限。
const maxSubagentContextItems = 20

// Deps 是 subagent_view 的装配输入。
type Deps struct {
	Core *state.Core
	// View 用于事件增量 revision bump（锁内调用）。
	View ViewPort
	// Limits 返回当前生效的运行时上限（截断配置）。
	Limits func() seelexctx.Limits
}

// ViewPort 是 Snapshot revision bump 的窄接口。
type ViewPort interface {
	BumpLocked() uint64
}

// Coordinator 拥有子代理详情/实时/树投影。
type Coordinator struct {
	*state.Core
	view   ViewPort
	limits func() seelexctx.Limits
}

// NewCoordinator 构造 subagent_view 协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{Core: deps.Core, view: deps.View, limits: deps.Limits}
}

// SubscribeSubagentLive 订阅 node 第一视角实时流（历史回放 + 只读通道 +
// 取消函数）。
func (c *Coordinator) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	if c.Deps.Engine == nil {
		return nil, nil, func() {}, fmt.Errorf("subagent live: engine unavailable")
	}
	if nodeID == "" {
		return nil, nil, func() {}, fmt.Errorf("subagent live: node id required")
	}
	return c.Deps.Engine.SubscribeSubagentLive(nodeID)
}

// HandleSubagentToolEvent 把 Runtime 工具分发投影进有界权威 Plan 节点快照
// 并发布一次前端增量。
func (c *Coordinator) HandleSubagentToolEvent(e seelsession.SubagentToolEvent) {
	if strings.TrimSpace(e.NodeID) == "" || strings.TrimSpace(e.ID) == "" {
		return
	}
	// runtime 事件与快照投影同一类型（dto.SubagentToolEvent 单源）；此处只做
	// 有界截断，不复制字段。
	e.Arguments = c.truncateSubagentEvidence(e.Arguments)
	e.Result = c.truncateSubagentEvidence(e.Result)
	e.Error = c.truncateSubagentEvidence(e.Error)

	c.Mu.Lock()
	plan := c.Snapshot.Runtime.Plan
	if plan == nil {
		c.Mu.Unlock()
		return
	}
	node := FindPlanNodeByID(plan.Nodes, e.NodeID)
	if node == nil {
		c.Mu.Unlock()
		return
	}
	c.upsertSubagentToolEvent(node, e)
	revision := c.view.BumpLocked()
	requestID := c.Snapshot.Chat.RequestID
	sessionID := c.Snapshot.Session.ID
	c.Mu.Unlock()

	kind := event.EventSubagentToolCompleted
	if e.Status == "running" {
		kind = event.EventSubagentToolStarted
	}
	if hub, ok := c.Events.(event.SessionAwareHub); ok {
		hub.PublishSession(kind, revision, requestID, sessionID, e)
	} else {
		c.Events.Publish(kind, revision, requestID, e)
	}
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

// SubagentDetail 返回节点子代理详情（截断会话 + 上下文快照 + worktree 现场；
// 只读子代理 actor，安全）。
func (c *Coordinator) SubagentDetail(nodeID string) (*model.SubagentDetail, error) {
	if nodeID == "" {
		return nil, fmt.Errorf("subagent detail: node id is required")
	}
	c.Mu.RLock()
	var status model.NodeStatus
	var elapsed, output string
	var toolEvents []model.SubagentToolEvent
	plan := c.Snapshot.Runtime.Plan
	if plan != nil {
		if node := FindPlanNodeByID(plan.Nodes, nodeID); node != nil {
			status = node.Status
			elapsed = node.Elapsed
			output = node.Output
			toolEvents = append([]model.SubagentToolEvent(nil), node.ToolEvents...)
		}
	}
	c.Mu.RUnlock()

	conversation, ok := c.Deps.Engine.NodeSessionConversation(nodeID)
	if !ok && status == "" {
		return nil, fmt.Errorf("subagent detail: node %q has no conversation", nodeID)
	}
	contextSnap, _ := c.Deps.Engine.NodeContextSnapshot(nodeID)
	detail := &model.SubagentDetail{
		Running:      isRunningSubagentStatus(status),
		Status:       status,
		Elapsed:      elapsed,
		Output:       output,
		Conversation: c.adaptSubagentConversation(conversation),
		ToolEvents:   toolEvents,
		Context:      c.adaptSubagentContext(contextSnap),
		Worktree:     c.nodeWorktreeInfo(nodeID),
	}
	return detail, nil
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

func (c *Coordinator) adaptSubagentContext(snap *snapshot.ContextSnapshot) *model.SubagentContext {
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

func (c *Coordinator) adaptSubagentConversation(messages []types.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	limit := c.limits().EvidenceChars
	adapted := make([]model.Message, 0, min(len(messages), maxSubagentConversationMessages))
	for _, msg := range messages {
		if len(adapted) >= maxSubagentConversationMessages {
			break
		}
		content := ""
		if msg.Content != nil {
			content = *msg.Content
		}
		if limit > 0 && len(content) > limit {
			content = content[:limit] + "…"
		}
		message := model.Message{Role: msg.Role, Content: content}
		if msg.Name != "" || msg.ToolCallID != "" {
			message.Tool = &model.ToolCall{
				ID: msg.ToolCallID, Name: msg.Name, Status: "completed",
			}
		}
		adapted = append(adapted, message)
	}
	return adapted
}
