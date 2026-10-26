package core

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 子代理中断恢复的应用层入口（headless/GUI 走这一面）：
//
//	subagent.list    只读表格：残留单元 + active/是否已有结论
//	subagent.recover 冷恢复续跑全部未完成子代理（七步模板）
//	subagent.resume  定点重试单个子代理（幂等键 = 节点 ID）
//
// 业务本体在 seelebridge（fork 派发/节点记录）与 application/core/resume
// （领域无关模板）；这里只做窄转发，不复制状态机。

// ListSubagentRecovery 列出目标会话下残留子代理单元的恢复态（只读）。
func (service *Service) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	return service.subagentApplicationComponent().ListRecovery(sessionID)
}

// ResumeInterruptedSubagents 冷恢复续跑目标会话下所有未完成子代理。
func (service *Service) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	return service.subagentApplicationComponent().ResumeInterrupted(ctx, sessionID)
}

// ResumeSubagent 定点续跑单个子代理（失败可重试）。
func (service *Service) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	return service.subagentApplicationComponent().Resume(ctx, sessionID, nodeID)
}

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

// ForkSubagents 直接派发一批子代理（自动化/冒烟入口；与模型调用
// fork_subagents 同一条执行链）。
func (service *Service) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	return service.subagentApplicationComponent().Fork(ctx, sessionID, specs)
}
