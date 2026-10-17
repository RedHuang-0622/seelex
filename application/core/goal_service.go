package core

// goal_service.go — Service 侧 goal 方法面与工具 handler（P1）。
//
// 每个方法按 ctx/显式 sessionID 路由到 components.goal 协调器；无 goal
// 协调器（未装配）时返回显式错误或 nil 视图。工具 handler 与 wiring/registry 的
// goal_begin/goal_update/goal_status/goal_done 注册配对。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

var errGoalCoordinatorUnavailable = errors.New("goal: 会话 goal 协调器未装配")

// GoalGovernanceViewFor 返回指定会话的 goal 治理只读视图（view_state 装配
// 端口；无 goal/未装配 → nil，前端隐藏面板）。
func (service *Service) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	if service == nil || service.components.goal == nil {
		return nil
	}
	return service.components.goal.GoalGovernanceViewFor(sessionID)
}

func (service *Service) goalCoordinatorFor(sessionID string) (*goalCoordinator, error) {
	if service == nil || service.components.goal == nil {
		return nil, errGoalCoordinatorUnavailable
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("goal: session ID is required")
	}
	return service.components.goal, nil
}

// GoalBeginFor 按显式会话注册 goal（多会话路由）。
//
// **不在 goal 上线时自动装配团队**（2026-10-01 删除 `ensureGoalAgentTeam`）。两个理由：
//  1. **它会砸掉本会话已有的团队**：装配 = 注册表整份替换 + lifecycle 顺序整份替换
//     （factory.Materialize → WriteTeamRegistry / SetLifecycleOrder），所以"开始一个 goal"
//     会把用户手工加的 worker/reviewer 一起冲成模板那三个人。这不是自动化的边界，是事故。
//  2. **团队是独立的产品事实**：会话里谁在编由用户/leader 决定（teamwork 的
//     team plan），不是 goal 生命周期的副产品。
func (service *Service) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Begin(ctx, sessionID, request)
	if err != nil {
		return nil, err
	}
	service.refreshGoalRuntimeProjection(sessionID)
	return record, nil
}

// teamJoinSeqFor 已随 `@` 手动召唤团队退场（2026-10-12 J-1a）删除：它的唯一调用方
// 是 application/core/input_team.go 的召唤落点（`MaterializeAgentTeam` 的 join 切点
// = 主会话当前已提交的 message 尾 seq）。会话团队装配面退场后没有装配来路，也就
// 没有"入伙切点"这个输入；角色会话号仍由 `RoleSnapshot` 那条既有读面提供（B1 保留
// 面，未动）。

// GoalBegin 按执行 ctx 会话注册 goal（main agent 工具调用路径）。
func (service *Service) GoalBegin(ctx context.Context, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return service.GoalBeginFor(ctx, sessionIDFromContext(ctx), request)
}

// GoalUpdateFor 按显式会话更新栈顶 goal。
func (service *Service) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Update(ctx, sessionID, request)
	if err == nil {
		service.refreshGoalRuntimeProjection(sessionID)
	}
	return record, err
}

// GoalUpdate 按执行 ctx 会话更新（main agent 工具调用路径）。
func (service *Service) GoalUpdate(ctx context.Context, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return service.GoalUpdateFor(ctx, sessionIDFromContext(ctx), request)
}

// GoalDoneFor 按显式会话**直接收口**栈顶 goal（goal_done 工具路径）。
//
// 口径：main agent 自己拍板收口——不送终态 gate、不产生提议（领域侧见
// goalCoordinator.FinishDirect）。teammate 子会话仍禁：
// goal 工具族对 subagent 整族不可见（backend/tool/registry/policy.go isGoalTool）。
//
// 收口后的收尾：刷新运行态投影。目标看板不在这里封板——它跟着审计流水走（goal.finish /
// goal.abort 由 closedInfo 从终态审计反解），多一条收口路径就是两份事实。
func (service *Service) GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.FinishDirect(ctx, sessionID, request, abort)
	if err == nil {
		service.refreshGoalRuntimeProjection(sessionID)
	}
	return record, err
}

// GoalDone 按执行 ctx 会话直接收口（main agent 工具调用路径）。
func (service *Service) GoalDone(ctx context.Context, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	return service.GoalDoneFor(ctx, sessionIDFromContext(ctx), request, abort)
}

// GoalStatusFor 按显式会话返回 goal 栈全量视图。
func (service *Service) GoalStatusFor(sessionID string) (goaldomain.StatusView, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return goaldomain.StatusView{}, err
	}
	return coordinator.StatusFor(sessionID), nil
}

// GoalNextFor / GoalNext / GoalBreakFor 已随席位轮转退场删除（2026-10-01 阶段三
// W3）：不再有可"推进一轮/中断"的治理循环。goal 的收口走 goal_done。

// refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
// 槽（GoalGovernanceView 进 Snapshot.Runtime），使治理面板在直接工具/headless
// 调用路径也立即可见（不依赖下一次 ChatStream 的回合尾投影）。
func (service *Service) refreshGoalRuntimeProjection(sessionID string) {
	if service == nil || service.components.view == nil {
		return
	}
	projection := service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID)
	service.ViewMu.Lock()
	service.components.view.ApplyRuntimeProjectionForLocked(sessionID, projection)
	service.ViewMu.Unlock()
}

// goalBeginHandler 是 goal_begin 工具 handler（wiring/registry 注册）。
func (service *Service) goalBeginHandler(ctx context.Context, argsJSON string) (string, error) {
	var request goaldomain.BeginRequest
	if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
		return "", fmt.Errorf("goal_begin: invalid arguments: %w", err)
	}
	record, err := service.GoalBegin(ctx, request)
	if err != nil {
		return "", err
	}
	return marshalGoalResult(record)
}

// goalUpdateHandler 是 goal_update 工具 handler（agent 工具面：权限收口见
// goal 工具族对 subagent/employee 不可见；人类/运维面的显式会话 API 是 GoalUpdateFor）。
func (service *Service) goalUpdateHandler(ctx context.Context, argsJSON string) (string, error) {
	var request goaldomain.UpdateRequest
	if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
		return "", fmt.Errorf("goal_update: invalid arguments: %w", err)
	}
	record, err := service.GoalUpdate(ctx, request)
	if err != nil {
		return "", err
	}
	return marshalGoalResult(record)
}

// goalDoneHandler 是 goal_done 工具 handler（main agent 的**真收口**面，口径见 GoalDoneFor）。
//
// 参数：action = finish | abort（缺省 finish）+ reason / result（审计）。action 取值刻意
// 只收这两个词表内的值：把 "done"/"complete" 这类同义词也放进来，就是让调用方猜。
func (service *Service) goalDoneHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("goal_done: invalid arguments: %w", err)
	}
	abort := false
	switch action := strings.ToLower(strings.TrimSpace(raw.Action)); action {
	case "", "finish":
	case "abort":
		abort = true
	default:
		return "", fmt.Errorf("goal_done: action 只能是 finish 或 abort（得到 %q）", raw.Action)
	}
	record, err := service.GoalDone(ctx, goaldomain.FinishRequest{Reason: raw.Reason, Result: raw.Result}, abort)
	if err != nil {
		return "", fmt.Errorf("goal_done: %w", err)
	}
	return marshalGoalResult(record)
}

// goalStatusHandler 是 goal_status 工具 handler。
func (service *Service) goalStatusHandler(ctx context.Context, _ string) (string, error) {
	status, err := service.GoalStatusFor(goalReadSessionID(ctx))
	if err != nil {
		return "", err
	}
	return marshalGoalResult(status)
}

func marshalGoalResult(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("goal: encode result: %w", err)
	}
	return string(raw), nil
}

// GoalBeginHandler / GoalUpdateHandler / GoalStatusHandler
// 是 goal 工具族的公开 handler（wiring/registry 注册；
// Seele 工具面签名：ctx + argsJSON → JSON 字符串）。
func (service *Service) GoalBeginHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.goalBeginHandler(ctx, argsJSON)
}

func (service *Service) GoalUpdateHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.goalUpdateHandler(ctx, argsJSON)
}

func (service *Service) GoalStatusHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.goalStatusHandler(ctx, argsJSON)
}

// GoalDoneHandler 是 goal_done 工具 handler（main agent 的真收口面；teammate 看不到这个
// 工具——goal 工具族对 subagent 整族不可见）。
func (service *Service) GoalDoneHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.goalDoneHandler(ctx, argsJSON)
}
