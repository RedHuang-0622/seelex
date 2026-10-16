// Package mapper 集中 seelebridge 运行态结构 ↔ contract/dto
// 的无业务转换（只做形状映射，不含截断/校验/策略）。
package mapper

import (
	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ToolEventToDTO 把子代理工具调用运行态事件投影为实时流对外 DTO：
// Duration（time.Duration）→ DurationMS（毫秒），供 SubagentLiveEvent 推送。
func ToolEventToDTO(event subagentsession.SubagentToolEvent) dto.SubagentTool {
	return dto.SubagentTool{
		ID: event.ID, NodeID: event.NodeID, Name: event.Name,
		Arguments: event.Arguments, Result: event.Result, Error: event.Error,
		Status: event.Status, StartedAt: event.StartedAt,
		DurationMS: event.Duration.Milliseconds(),
	}
}
