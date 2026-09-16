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
	"log"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
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
	if err != nil {
		return nil, err
	}
	// goal 上线即拉起它的 A2A 团队：goal 的 TL/ADVISOR 是 AgentTeam 工厂的
	// 第一个实例（preset goal-a2a，TL 的 JoinPolicy=on_goal_create），因此这条
	// 装配不该等前端手动点一次「装配团队」。
	service.ensureGoalAgentTeam(sessionID)
	service.refreshGoalRuntimeProjection(sessionID)
	return record, nil
}

// ensureGoalAgentTeam 确保当前会话的 goal-a2a 团队已装配（幂等）。
//
// 幂等来源在工厂：同一个 (team_id, role_name) 派生稳定的 role_session_id，
// 重复创建 goal 不会产生第二个角色会话；注册表与 lifecycle 顺序整份替换，
// 重复装配结果一致。
//
// best-effort：宿主未装配团队存储（旧版本宿主、测试桩）时只记一条日志，
// 不阻塞 goal 治理本身——goal 的 supervisor + TL 评估器链路与团队存储无关。
//
// joinSeq 取装配那一刻主会话已提交的 message 尾 seq：角色会话可见区间（= 它自己
// 那份 team work 记录）的判据是 seq > join_seq_id（与 storage 侧 assembleRoleWire
// 同一条），所以 teammate 的记录从"装配它的那一回合"开始——它入伙之前的对话不在
// 它的前缀匹配区间里（面板上以占位呈现），而不是把整段历史都算成它记得的上下文。
func (service *Service) ensureGoalAgentTeam(sessionID string) {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if _, err := service.MaterializeAgentTeamPreset(sessionID, dto.TeamKindGoalA2A, service.teamJoinSeqFor(sessionID)); err != nil {
		log.Printf("[goal] 自动装配 %s 团队失败（session=%s）：%v", dto.TeamKindGoalA2A, sessionID, err)
	}
}

// teamJoinSeqFor 返回团队装配的 join 切点 = 主会话当前已提交的 message 尾 seq。
// 读不到（宿主未装配会话存储、会话还没落行）时退回 0 = 挂在主会话可见起点：
// 宁可让 teammate 多看到一段，也不给它一个假切点。
func (service *Service) teamJoinSeqFor(sessionID string) uint64 {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return 0
	}
	snapshot, err := service.RoleSnapshot(sessionID, RoleNameMain, sessionID)
	if err != nil {
		return 0
	}
	return snapshot.MainHeadSeq
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
		Detail: service.goalTurnWorkSummary(sessionID),
	})
	directives := coordinator.DrainDirectives(sessionID)
	if len(directives) == 0 {
		return true
	}
	service.injectGoalDirectives(sessionID, directives)
	return true
}

// formatDirectiveText 是 b→a 指令的**单行可读形式**：引擎受信注入与可见回放
// 共用同一份格式（两处各拼一遍字符串必然漂移，corr 是唯一的行标识）。
func formatDirectiveText(directive goaldomain.TLDirective) string {
	return "[TL 指令 " + directive.Corr + "] " + strings.TrimSpace(directive.Content)
}

// injectGoalDirectives 把 b→a 指令注入引擎受信区，并登记"待可见回放"：
//
//   - 注入：以 user 角色写进引擎历史（下一次模型调用就能看到），包在〔〕里
//     与真实用户输入区分；
//   - 登记：同一批指令记进 coordinator.injections，回合尾由
//     injectGoalDirectivesFor 回放进可见会话（引擎历史不是可见投影的事实源）。
func (service *Service) injectGoalDirectives(sessionID string, directives []goaldomain.TLDirective) {
	for _, directive := range directives {
		value := "〔" + formatDirectiveText(directive) + "〕"
		service.appendEngineMessage(sessionID, types.Message{Role: "user", Content: &value})
	}
	service.components.goal.NoteInjected(sessionID, directives)
}

// injectGoalDirectivesForStart 在 ChatStream 开始前把 TL 回合产生的指令
// 排空并注入引擎受信区（可见副本的两种出口见 injectGoalDirectivesFor 与
// publishPendingGoalDirectivesFor）。
func (service *Service) injectGoalDirectivesForStart(sessionID string) {
	if service == nil || service.components.goal == nil {
		return
	}
	directives := service.components.goal.DrainDirectives(sessionID)
	if len(directives) == 0 {
		return
	}
	service.injectGoalDirectives(sessionID, directives)
}

// goalAdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进 goal 治理
// （turn 结束 → TL 回合），让 A2A 在真实会话中可见（Round/Peer/指令）。本轮
// EXEC 的工作正文摘要随 turn_completed 登记，ADVISOR 下一回合据此评审真实产出。
func (service *Service) goalAdvanceAfterChat(ctx context.Context) {
	sessionID := sessionIDFromContext(ctx)
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return
	}
	_ = coordinator.AdvanceAfterChat(ctx, sessionID, service.goalTurnWorkSummary(sessionID))
}

// injectGoalDirectivesFor 在 ChatStream 结束后的锁外安全点，把本回合已注入
// 引擎的 TL 指令回放进可见会话（仅展示，不入 goal 栈）。
func (service *Service) injectGoalDirectivesFor(sessionID string) {
	if service == nil || service.components.goal == nil {
		return
	}
	service.publishAdvisorDirectiveRows(sessionID, service.components.goal.TakeInjected(sessionID))
}

// publishPendingGoalDirectivesFor 把治理回合**刚产出**、仍在待注入队列里的
// b→a 指令立刻回放进可见会话。
//
// 为什么需要它：治理回合（ADVISOR）跑在回合末尾（goalAdvanceAfterChat），它
// 产出的裁决过去只在**下一次**用户提交时才被排空注入、再在下一次回合尾回放
// ——用户盯着面板也看不到裁决（2026-09-16 team work 探针实测：等满 15 分钟
// 仍无行）。现在：裁决在产出它的那一回合就可见。
//
// 指令本身不消费（PeekDirectives）：受信注入仍由下一次 ChatStream 前的
// DrainDirectives 完成，注入语义与时机不变；已回放的 corr 记账在 coordinator
// 里，下一次回合的常规回放据此去重，同一裁决只出现一行。
func (service *Service) publishPendingGoalDirectivesFor(sessionID string) {
	if service == nil || service.components.goal == nil {
		return
	}
	service.publishAdvisorDirectiveRows(sessionID, service.components.goal.PeekDirectives(sessionID))
}

// publishAdvisorDirectiveRows 把 b→a 指令以可见 ADVISOR 行写进目标会话：
// role=assistant + role_name=tl + kind=tl_directive（写成 system 行会让聊天区
// 把它渲染成「系统」，两个 agent 又变回无区别，见
// visible_role_attribution_test.go）。同一 corr 只写一次：指令产出的那一回合
// 就该可见，下一次回合的常规回放不得把它再写一遍。
func (service *Service) publishAdvisorDirectiveRows(sessionID string, directives []goaldomain.TLDirective) {
	published := make([]goaldomain.TLDirective, 0, len(directives))
	for _, directive := range directives {
		if service.components.goal.DirectivePublished(sessionID, directive.Corr) {
			continue
		}
		published = append(published, directive)
	}
	if len(published) == 0 {
		return
	}
	// 角色会话号解析走存储读（锁外完成，避免在 ViewMu 里做 I/O）。
	advisorSessionID := service.advisorRoleSessionID(sessionID)
	roundID := service.components.tasks.RoleRoundFor(sessionID)
	service.ViewMu.Lock()
	for _, directive := range published {
		origin := MessageOrigin{
			RoleName: RoleNameTL, RoleSessionID: advisorSessionID,
			RoundID: roundID, Kind: goaldomain.DirectiveRowKind,
		}
		service.appendSessionMessageWithOriginLocked(sessionID, "assistant", formatDirectiveText(directive), nil, origin)
	}
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	for _, directive := range published {
		service.components.goal.MarkDirectivePublished(sessionID, directive.Corr)
	}
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
}

// advisorRoleSessionID 解析 ADVISOR（tl）的角色会话号：按工厂口径
// (team_id, role_name) 派生，与 goal 回合记录器写入 draft 的会话号同源。
// 未装配 AgentTeam（旧会话/测试桩）时返回空——不伪造角色会话号。
func (service *Service) advisorRoleSessionID(sessionID string) string {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	view, err := service.AgentTeamView(sessionID)
	if err != nil || !view.Configured {
		return ""
	}
	return agentteam.RoleSessionID(view.TeamID, RoleNameTL)
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
