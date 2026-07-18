package mcpstack

import (
	"encoding/json"
	"fmt"

	frameworkmcp "github.com/RedHuang-0622/Seele/agent/core/tool/mcp"
)

// ListenBreaker 消费熔断器事件 channel，将状态变更记录到 MCPStack trace 中。
// 每条熔断事件被记录为一条特殊的 MCPCall（StatusRolledBack 标记），
// 这样调用历史里既有普通调用记录，也有熔断状态变化记录。
//
// 用法：
//
//	ch := runtime.BreakerEvents()
//	go mcpstack.ListenBreaker(traceStack, ch)
func ListenBreaker(s *MCPStack, ch <-chan frameworkmcp.BreakerEvent) {
	for evt := range ch {
		// 将熔断事件编码为一条特殊 trace 记录
		detail := map[string]interface{}{
			"event":    string(evt.Type),
			"failures": evt.Failures,
		}
		detailJSON, _ := json.Marshal(detail)

		call := MCPCall{
			ID:         fmt.Sprintf("breaker-%s-%s", evt.ServerName, evt.Type),
			ServerName: evt.ServerName,
			ToolName:   fmt.Sprintf("__breaker__%s", evt.Type),
			Args:       detailJSON,
			Status:     StatusRolledBack,
			TokenCount: 10,
		}

		switch evt.Type {
		case frameworkmcp.BreakerOpened:
			call.ErrorMsg = fmt.Sprintf("熔断器打开，连续失败 %d 次", evt.Failures)
		case frameworkmcp.BreakerHalfOpen:
			call.ErrorMsg = "退避到期，半开探测"
		case frameworkmcp.BreakerClosed:
			call.ErrorMsg = ""
		case frameworkmcp.BreakerRecovering:
			call.ErrorMsg = "后台 ping 恢复中"
		case frameworkmcp.BreakerRecovered:
			call.ErrorMsg = "后台 ping 恢复成功"
		}

		_ = s.Record(call) // best-effort
	}
}
