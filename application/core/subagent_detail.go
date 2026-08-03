package core

import (
	"fmt"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/model"
)

// ── 子代理详情查看（切片 8，docs/plan/subagent-detail-architecture.md）──
// 点击 plan 节点卡片 → 详情弹窗"会话记录"标签：子代理自己的对话流
// （user/assistant/工具调用/工具结果）+ 状态/耗时/输出（复用 PlanNode）。
// 数据面：Runtime 节点会话注册表（运行中实时 / 结束后快照，只读子代理
// actor，安全——绝不触碰主会话锁，死锁教训见 actor.go）。

// maxSubagentConversationMessages 单节点详情返回的会话消息上限
// （防超长节点会话撑爆详情响应；可经 limits 扩展）。
const maxSubagentConversationMessages = 50

// SubagentSessionDetail 返回节点子代理的详情数据：
//   - Conversation：子代理会话记录（截断：单条 ≤ evidence_chars、总 ≤ 50 条）；
//   - Running：是否执行中（实时读 vs 结束后快照）；
//   - Status/Elapsed/Output：复用 PlanNode 投影。
func (service *Service) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error) {
	if nodeID == "" {
		return nil, fmt.Errorf("subagent detail: node id is required")
	}
	service.mu.RLock()
	var status model.NodeStatus
	var elapsed, output string
	plan := service.snapshot.Runtime.Plan
	if plan != nil {
		for i := range plan.Nodes {
			if plan.Nodes[i].ID == nodeID {
				status = plan.Nodes[i].Status
				elapsed = plan.Nodes[i].Elapsed
				output = plan.Nodes[i].Output
				break
			}
		}
	}
	service.mu.RUnlock()

	conversation, ok := service.deps.Engine.NodeSessionConversation(nodeID)
	if !ok && status == "" {
		return nil, fmt.Errorf("subagent detail: node %q has no conversation", nodeID)
	}
	detail := &model.SubagentDetail{
		Running:      status == model.NodeRunning,
		Status:       status,
		Elapsed:      elapsed,
		Output:       output,
		Conversation: adaptSubagentConversation(conversation),
	}
	return detail, nil
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
