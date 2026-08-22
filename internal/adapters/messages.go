package adapters

import (
	"context"
	"strings"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
)

func adaptMessages(messages []types.Message) []contract.EngineMessage {
	result := make([]contract.EngineMessage, 0, len(messages))
	for _, message := range messages {
		adapted := contract.EngineMessage{
			Role: message.Role, ReasoningContent: message.ReasoningContent,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if message.Content != nil {
			adapted.Content = *message.Content
			adapted.ContentSet = true
		}
		adapted.ToolCalls = make([]contract.EngineToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			adapted.ToolCalls = append(adapted.ToolCalls, contract.EngineToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
		result = append(result, adapted)
	}
	return result
}

func restoreMessages(messages []contract.EngineMessage) []types.Message {
	result := make([]types.Message, 0, len(messages))
	for _, message := range messages {
		adapted := types.Message{
			Role: message.Role, ReasoningContent: message.ReasoningContent,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if message.ContentSet || message.Content != "" {
			content := message.Content
			adapted.Content = &content
		}
		for _, call := range message.ToolCalls {
			adapted.ToolCalls = append(adapted.ToolCalls, types.ToolCall{
				ID: call.ID, Type: "function",
				Function: types.ToolCallFunction{Name: call.Name, Arguments: call.Arguments},
			})
		}
		result = append(result, adapted)
	}
	return result
}

func ApprovalOption(choice string) model.InteractionOption {
	options := map[string]model.InteractionOption{
		"execute": {ID: "execute", Label: "执行", Description: "按计划执行", Style: "primary"},
		"skip":    {ID: "skip", Label: "跳过", Description: "跳过当前节点", Style: "secondary"},
		"abort":   {ID: "abort", Label: "终止", Description: "终止工作流", Style: "danger"},
		"confirm": {ID: "confirm", Label: "确认", Description: "确认并继续", Style: "primary"},
		"retry":   {ID: "retry", Label: "重试", Description: "重新执行", Style: "warning"},
	}
	if option, ok := options[choice]; ok {
		return option
	}
	return model.InteractionOption{ID: choice, Label: choice}
}

func ApprovalAccepted(optionID string) bool {
	switch strings.ToLower(strings.TrimSpace(optionID)) {
	case "", "__cancel__", "__timeout__", "no", "deny", "reject", "refuse", "cancel", "abort", "skip", "false", "否", "拒绝", "取消", "终止", "跳过":
		return false
	default:
		return true
	}
}

// planApprovalGate 适配框架 approve.ApprovalGate → approval.ApprovalBroker。
// plan_run 执行到 kind:manual 节点时，框架调用 Ask 阻塞等待用户在 UI 中选择。
type PlanApprovalGate struct {
	Broker *approval.ApprovalBroker
}

// Ask 将框架审批请求转换为 ApprovalBroker.Request，阻塞等待用户选择后返回。
func (g *PlanApprovalGate) Ask(ctx context.Context, q approve.Question) (any, error) {
	options := make([]model.InteractionOption, len(q.Options))
	for i, opt := range q.Options {
		options[i] = model.InteractionOption{
			ID: opt.Key, Label: opt.Label,
			Description: opt.Description, Style: opt.Style,
		}
	}

	req := approval.ApprovalRequest{
		ID:       q.ID,
		Question: q.Content,
		Options:  options,
		Timeout:  q.Timeout,
	}

	decision, err := g.Broker.Request(ctx, req)
	if err != nil {
		return "", err
	}
	return decision.OptionID, nil
}

// convertPermissionOptions 将 permission.ApproveOption 转为 model.InteractionOption。
func ConvertPermissionOptions(opts []toolspermission.ApproveOption) []model.InteractionOption {
	out := make([]model.InteractionOption, len(opts))
	for i, o := range opts {
		out[i] = model.InteractionOption{
			ID: o.Key, Label: o.Label, Description: o.Description, Style: o.Style,
		}
	}
	return out
}
