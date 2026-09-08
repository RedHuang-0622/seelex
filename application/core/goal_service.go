package core

// goal_service.go — Service 侧 goal 方法面与工具 handler（P1）。
//
// 每个方法按 ctx/显式 sessionID 路由到 components.goal 协调器；无 goal
// 协调器（未装配）时返回显式错误或 nil 视图。工具 handler 与 main.go 的
// goal_begin/goal_update/goal_status/goal_propose_finish 注册配对。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
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
func (service *Service) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Begin(ctx, sessionID, request)
	if err == nil {
		service.refreshGoalRuntimeProjection(sessionID)
	}
	return record, err
}

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

// GoalProposeFinishFor 按显式会话送终态 gate。
func (service *Service) GoalProposeFinishFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return goaldomain.FinishProposalResult{}, err
	}
	result, err := coordinator.ProposeFinish(ctx, sessionID, request)
	if err == nil {
		service.refreshGoalRuntimeProjection(sessionID)
	}
	return result, err
}

// GoalProposeFinish 按执行 ctx 会话提议收口（main agent 工具调用路径）。
func (service *Service) GoalProposeFinish(ctx context.Context, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error) {
	return service.GoalProposeFinishFor(ctx, sessionIDFromContext(ctx), request)
}

// GoalStatusFor 按显式会话返回 goal 栈全量视图。
func (service *Service) GoalStatusFor(sessionID string) (goaldomain.StatusView, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return goaldomain.StatusView{}, err
	}
	return coordinator.StatusFor(sessionID), nil
}

// GoalNextFor 按显式会话推进一轮治理循环。
func (service *Service) GoalNextFor(ctx context.Context, sessionID string) (bool, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return false, err
	}
	more, err := coordinator.Next(ctx, sessionID)
	if err == nil {
		service.refreshGoalRuntimeProjection(sessionID)
	}
	return more, err
}

// GoalNext 按执行 ctx 会话推进治理循环。
func (service *Service) GoalNext(ctx context.Context) (bool, error) {
	return service.GoalNextFor(ctx, sessionIDFromContext(ctx))
}

// SetGoalTLEvaluator 注入真实 TL 评估器（组合根：seelebridge 账号面 →
// goal 域 TLEvaluator；首次会话启动前调用）。
func (service *Service) SetGoalTLEvaluator(evaluator goaldomain.TLEvaluator) {
	if service == nil || service.components.goal == nil {
		return
	}
	service.components.goal.setEvaluator(evaluator)
}

// GoalBreakFor 按显式会话外部中断治理循环（headless goal_gov_break）。
func (service *Service) GoalBreakFor(_ context.Context, sessionID, reason string) error {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return err
	}
	if err := coordinator.Break(context.Background(), sessionID, reason); err != nil {
		return err
	}
	service.refreshGoalRuntimeProjection(sessionID)
	return nil
}

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

// GoalIterationCompleted 是 ChatStream OnIterationComplete 的 goal 接线：
// 登记 turn_completed（exec 账本水位），排空 b→a 指令并注入引擎历史
// （Session 锁内只允许 AppendHistory，见 contract.ChatEngine 注释）。返回
// true 不阻断主循环（B4：a 永不等待 b）。
func (service *Service) GoalIterationCompleted(ctx context.Context) bool {
	sessionID := sessionIDFromContext(ctx)
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return true
	}
	if coordinator.StatusFor(sessionID).Active == nil {
		return true
	}
	_ = coordinator.Notify(ctx, sessionID, goaldomain.TLEvalSignal{
		Kind: goaldomain.SignalTurnCompleted, Source: "iteration_complete",
	})
	directives := coordinator.DrainDirectives(sessionID)
	if len(directives) == 0 {
		return true
	}
	var texts []string
	for _, directive := range directives {
		text := "[TL 指令 " + directive.Corr + "] " + strings.TrimSpace(directive.Content)
		texts = append(texts, text)
		value := "〔" + text + "〕"
		service.appendEngineMessage(sessionID, types.Message{Role: "user", Content: &value})
	}
	coordinator.NoteInjected(sessionID, texts)
	return true
}

// injectGoalDirectivesForStart 在 ChatStream 开始前把 TL 回合产生的指令
// 排空并注入引擎历史（受信注入区；visible 记录在回合尾回放）。
func (service *Service) injectGoalDirectivesForStart(sessionID string) {
	if service == nil || service.components.goal == nil {
		return
	}
	directives := service.components.goal.DrainDirectives(sessionID)
	if len(directives) == 0 {
		return
	}
	var texts []string
	for _, directive := range directives {
		text := "[TL 指令 " + directive.Corr + "] " + strings.TrimSpace(directive.Content)
		texts = append(texts, text)
		value := "〔" + text + "〕"
		service.appendEngineMessage(sessionID, types.Message{Role: "user", Content: &value})
	}
	service.components.goal.NoteInjected(sessionID, texts)
}

// goalAdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进 goal 治理
// （turn 结束 → TL 回合），让 A2A 在真实会话中可见（Round/Peer/指令）。
func (service *Service) goalAdvanceAfterChat(ctx context.Context) {
	sessionID := sessionIDFromContext(ctx)
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return
	}
	_ = coordinator.AdvanceAfterChat(ctx, sessionID)
}

// injectGoalDirectivesFor 在 ChatStream 结束后的锁外安全点，把本回合已注入
// 引擎的 TL 指令以可见系统记录写入目标会话视图（仅展示，不入 goal 栈）。
func (service *Service) injectGoalDirectivesFor(sessionID string) {
	if service == nil || service.components.goal == nil {
		return
	}
	texts := service.components.goal.TakeInjected(sessionID)
	if len(texts) == 0 {
		return
	}
	service.ViewMu.Lock()
	for _, text := range texts {
		service.appendSessionMessageLocked(sessionID, "system", text, nil)
	}
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
}

// goalBeginHandler 是 goal_begin 工具 handler（main.go 注册）。
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

// goalUpdateHandler 是 goal_update 工具 handler。
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

// goalProposeFinishHandler 是 goal_propose_finish 工具 handler。
func (service *Service) goalProposeFinishHandler(ctx context.Context, argsJSON string) (string, error) {
	var request goaldomain.FinishRequest
	if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
		return "", fmt.Errorf("goal_propose_finish: invalid arguments: %w", err)
	}
	proposal, err := service.GoalProposeFinish(ctx, request)
	if err != nil {
		return "", err
	}
	return marshalGoalResult(proposal)
}

// goalStatusHandler 是 goal_status 工具 handler。
func (service *Service) goalStatusHandler(ctx context.Context, _ string) (string, error) {
	status, err := service.GoalStatusFor(sessionIDFromContext(ctx))
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

// GoalBeginHandler / GoalUpdateHandler / GoalStatusHandler /
// GoalProposeFinishHandler 是 goal 工具族的公开 handler（main.go 注册；
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

func (service *Service) GoalProposeFinishHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.goalProposeFinishHandler(ctx, argsJSON)
}
