package core

import (
	"fmt"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── 子代理详情查看（切片 8，docs/plan/subagent-detail-architecture.md）──
// 点击 plan 节点卡片 → 详情弹窗"会话记录"标签：子代理自己的对话流
// （user/assistant/工具调用/工具结果）+ 状态/耗时/输出（复用 PlanNode）。
// 数据面：Runtime 节点会话注册表（运行中实时 / 结束后快照，只读子代理
// actor，安全——绝不触碰主会话锁，死锁教训见 actor.go）。

// maxSubagentConversationMessages 单节点详情返回的会话消息上限
// （防超长节点会话撑爆详情响应；可经 limits 扩展）。
const maxSubagentConversationMessages = 50

// maxSubagentContextItems 上下文快照单类条目上限（发现/决策/约束/待办）。
const maxSubagentContextItems = 20

// SubagentSessionDetail 返回节点子代理的详情数据：
//   - Conversation：子代理会话记录（截断：单条 ≤ evidence_chars、总 ≤ 50 条）；
//   - Context：结构化上下文快照（Goal/Findings/Decisions/Constraints/
//     PendingWork/TokenEstimate，运行中实时导出、结束后快照；只读子代理
//     actor，安全）；
//   - Running：是否执行中（实时读 vs 结束后快照）；
//   - Status/Elapsed/Output：复用 PlanNode 投影。
func (service *Service) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error) {
	if nodeID == "" {
		return nil, fmt.Errorf("subagent detail: node id is required")
	}
	service.mu.RLock()
	var status model.NodeStatus
	var elapsed, output string
	var toolEvents []model.SubagentToolEvent
	plan := service.snapshot.Runtime.Plan
	if plan != nil {
		if node := findPlanNodeByID(plan.Nodes, nodeID); node != nil {
			status = node.Status
			elapsed = node.Elapsed
			output = node.Output
			toolEvents = append([]model.SubagentToolEvent(nil), node.ToolEvents...)
		}
	}
	service.mu.RUnlock()

	conversation, ok := service.deps.Engine.NodeSessionConversation(nodeID)
	if !ok && status == "" {
		return nil, fmt.Errorf("subagent detail: node %q has no conversation", nodeID)
	}
	contextSnap, _ := service.deps.Engine.NodeContextSnapshot(nodeID)
	detail := &model.SubagentDetail{
		Running:      isRunningSubagentStatus(status),
		Status:       status,
		Elapsed:      elapsed,
		Output:       output,
		Conversation: adaptSubagentConversation(conversation),
		ToolEvents:   toolEvents,
		Context:      adaptSubagentContext(contextSnap),
	}
	return detail, nil
}

// adaptSubagentContext 适配子代理上下文快照为只读 DTO：
// 单条文本截断到 evidence_chars，单类条目 ≤ maxSubagentContextItems。
func adaptSubagentContext(snap *snapshot.ContextSnapshot) *model.SubagentContext {
	if snap == nil {
		return nil
	}
	limit := Limits().EvidenceChars
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

// adaptSubagentConversation 适配子代理会话记录：单条内容截断到
// evidence_chars（limits），总条数 ≤ maxSubagentConversationMessages。
// 工具调用/结果的细节经 Tool 字段携带（assistant 的 tool_calls 摘要）。
func adaptSubagentConversation(messages []types.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	limit := Limits().EvidenceChars
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
