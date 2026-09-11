package sessionstore

import "github.com/RedHuang-0622/Seele/types"

// ProviderWireMessages 把 durable 记录投影出的 provider 历史对齐到框架**实际
// 发出**的 wire 形状（"重投影 == 已发出"，provider 前缀缓存成立的前提）。
//
// 规则：**携带工具调用的 assistant 消息，其正文恒为空**。框架构造该消息时直接
// 置 nil（Seele `session/loop.go:564`
// `types.Message{Role:"assistant", Content:nil, ToolCalls: toolCalls}`），provider
// 从未收到这段正文；而 durable 记录里该事件的正文是 **record-only** 的迭代说明
// 文本（在工具钩子边界按迭代归位，见
// application/core/task_context.AttributeToolNarrationLocked）——视图、轨迹与
// 重启恢复需要它，provider 请求不需要。
//
// 为什么必须在**这一层**做：生产装配下框架的 WorkingHistory 来自
// `DurableHistory.Load`（`ReleaseWorkingHistoryFor` 每轮清空工作视图，下一轮
// 从 durable owner 冷载尾窗，见 internal/adapters/engine_port.go:631-651），
// 因此这里才是 provider 请求的真实投影出口。把记录侧正文带进请求，同一会话的
// 后续请求就不再以前一条请求的字节为前缀（工具轮消息跨轮从空变成说明文本）。
// 真实 API 冒烟实测（DeepSeek，2026-09-12）：跨轮在 msg#1 丢掉 80–97 字节
// 前缀，命中率随之掉到 81%（见 real_api_prefix_live_test.go）。
//
// 与框架 wire 行为耦合：本规则成立的前提是"框架在 tool_calls 时丢弃正文"。
// 若 Seele 改为在 wire 上保留工具轮正文，必须同步撤销本规则（否则分叉方向
// 反转）。应用侧还有一处同族实现
// （application/core/context_runtime.RepairEmptyHistoryContent，作用于引擎
// 历史路径）；两处共同保证 provider 投影不写入 wire 上从未存在的字节。
func ProviderWireMessages(messages []types.Message) []types.Message {
	changed := false
	for index := range messages {
		message := &messages[index]
		if message.Role == "assistant" && len(message.ToolCalls) > 0 && message.Content != nil {
			changed = true
			break
		}
	}
	if !changed {
		return messages
	}
	projected := make([]types.Message, len(messages))
	copy(projected, messages)
	for index := range projected {
		message := &projected[index]
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			message.Content = nil
		}
	}
	return projected
}
