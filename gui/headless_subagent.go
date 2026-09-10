package gui

// headless_subagent.go 承载 headless 的 subagent.* 段（工作流 A 的文件归属）：
// 子代理是劳务派遣式 tool calling 能力，它的 active 只活跃在表格；这里暴露
// 「表格只读投影」与「中断后冷恢复续跑」两类调用，不提供任何 AgentTeam
// 成员/顺序语义（那些归 role.*/team.*）。
//
// 方法：
//
//	subagent.list    [sessionID]            → []SubagentRecoveryView
//	subagent.fork    [sessionID, specs[]]   → 派发结果摘要（自动化/冒烟派发）
//	subagent.recover [sessionID]            → SubagentResumeReport（全部未完成）
//	subagent.resume  [sessionID, nodeID]    → SubagentResumeResult（定点重试）

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// subagentRPCApplication 是 headless 需要的子代理恢复面（Application 的
// 窄子集；未装配时显式报错，不静默降级）。
type subagentRPCApplication interface {
	ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
	ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)
	ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)
}

func (server *headlessServer) dispatchSubagent(method string, args []json.RawMessage) (any, error) {
	app, ok := server.app.(subagentRPCApplication)
	if !ok {
		return nil, fmt.Errorf("%s: 当前 Application 未装配子代理恢复面", method)
	}
	stringArg := func(index int, name string) (string, error) {
		if index >= len(args) {
			return "", fmt.Errorf("%s 缺少参数 %s", method, name)
		}
		var value string
		if err := json.Unmarshal(args[index], &value); err != nil {
			return "", fmt.Errorf("%s 参数 %s 必须是字符串: %v", method, name, err)
		}
		return value, nil
	}
	switch method {
	case "subagent.list":
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		return app.ListSubagentRecovery(sessionID)
	case "subagent.fork":
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		if len(args) < 2 {
			return nil, fmt.Errorf("%s 缺少参数 subagents", method)
		}
		var specs []dto.SubagentForkSpec
		if err := json.Unmarshal(args[1], &specs); err != nil {
			return nil, fmt.Errorf("%s 参数 subagents 解码失败: %w", method, err)
		}
		return app.ForkSubagents(context.Background(), sessionID, specs)
	case "subagent.recover":
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		return app.ResumeInterruptedSubagents(context.Background(), sessionID)
	case "subagent.resume":
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		nodeID, err := stringArg(1, "nodeID")
		if err != nil {
			return nil, err
		}
		return app.ResumeSubagent(context.Background(), sessionID, nodeID)
	default:
		return nil, fmt.Errorf("未知 subagent headless 方法: %s", method)
	}
}
