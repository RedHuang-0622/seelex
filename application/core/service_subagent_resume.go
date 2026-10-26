package core

import (
	"fmt"
)

// 子代理中断恢复的应用层入口（headless/GUI 走这一面）：
//
//	subagent.list    只读表格：残留单元 + active/是否已有结论
//	subagent.recover 冷恢复续跑全部未完成子代理（七步模板）
//	subagent.resume  定点重试单个子代理（幂等键 = 节点 ID）
//
// 业务本体在 seelebridge（fork 派发/节点记录）与 application/core/resume
// （领域无关模板）；这里只做窄转发，不复制状态机。

// PrepareProviderHistory 补齐目标会话 provider 历史的残缺工具链：中断的
// 工具调用（含 subagent 派发）缺失结果 → provider-only tool 占位（成对
// 合法，UI 不渲染为真实输出）。子代理恢复模板的「补历史」步骤经组合根
// 注入到 seelebridge（Runtime.SetSubagentParentRepairer）。
func (service *Service) PrepareProviderHistory(sessionID string) error {
	if service == nil || service.components.history == nil {
		return fmt.Errorf("prepare provider history: history coordinator is not assembled")
	}
	// 这条入口在回合之外（子代理恢复的补历史步骤），传 nil 按锁外路径取锁。
	return service.components.history.PrepareProviderHistoryFor(sessionID)
}
