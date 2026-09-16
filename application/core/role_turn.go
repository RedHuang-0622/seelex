// role_turn.go 把跨层的「角色回合执行体」端口（contract.RoleTurnPort）适配成
// 治理循环消费的员工执行面（RoleTurnRunner）。
//
// 分层口径：application/core 声明"谁有座位、谁先谁后"（seatsFor / roleTurnSeat），
// 真正的执行体（角色会话 + 工具面 + 权责落地）在 seelebridge 侧。两者之间只走
// contract 的纯 DTO，core 不 import seelebridge（防腐层纪律）。
//
// 未装配端口（Deps.RoleTurn == nil）= 试水形态：agent 角色只占发言位，不推进治理
// 循环；座位派生因此不给 agent 座位（宁可少一座，不要假一座）。
package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// contractRoleTurnRunner 是 contract.RoleTurnPort → RoleTurnRunner 的适配。
type contractRoleTurnRunner struct {
	port contract.RoleTurnPort
}

func (runner contractRoleTurnRunner) RunRoleTurn(ctx context.Context, request RoleTurnRequest) (RoleTurnOutcome, error) {
	outcome, err := runner.port.RunRoleTurn(ctx, dto.RoleTurnRequest{
		SessionID:        request.SessionID,
		RoleName:         request.RoleName,
		RoleSessionID:    request.RoleSessionID,
		ToolsPolicy:      request.ToolsPolicy,
		PermissionGroups: request.PermissionGroups,
		OrderIndex:       request.OrderIndex,
		Input:            request.Input,
	})
	if err != nil {
		return RoleTurnOutcome{}, err
	}
	return RoleTurnOutcome{Ran: outcome.Ran, Progress: outcome.Progress, Note: outcome.Note}, nil
}

// roleTurnRunnerFor 返回该会话的员工执行面（未装配端口 → nil）。
//
// 只按"端口装没装"判断，不做会话级过滤：谁真的生成座位由座位派生决定
// （seatsFor 按注册表 kind），执行面只负责"轮到谁就把谁跑起来"。在这里再做一次
// 会话级判断会把两处判定拆成两份事实，早晚打架。
func (service *Service) roleTurnRunnerFor(string) RoleTurnRunner {
	if service == nil || service.Deps.RoleTurn == nil {
		return nil
	}
	return contractRoleTurnRunner{port: service.Deps.RoleTurn}
}
