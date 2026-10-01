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
//
// **不在 goal 上线时自动装配团队**（2026-10-01 删除 `ensureGoalAgentTeam`）。两个理由：
//  1. **它会砸掉本会话已有的团队**：装配 = 注册表整份替换 + lifecycle 顺序整份替换
//     （factory.Materialize → WriteTeamRegistry / SetLifecycleOrder），所以"开始一个 goal"
//     会把用户手工加的 worker/reviewer 一起冲成模板那三个人。这不是自动化的边界，是事故。
//  2. **goal 的评估链不依赖它**：没有装配团队时 `seatsFor` 返回空，治理循环走
//     `goaldomain.NewTurnGovernorForDSA2A`（EXEC + ADVISOR(supervisor)），TL 裁决照常在。
//     团队席位是**在编员工各自的回合**这一层的增量，属于"会话里谁在编"这件产品事实，
//     该由用户/leader 决定（leader-worker 目标态里由 team plan 决定）。
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
		service.dismissTeamWhenGoalClosed(sessionID)
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
		service.dismissTeamWhenGoalClosed(sessionID)
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
	service.dismissTeamWhenGoalClosed(sessionID)
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
// 只登记 turn_completed（exec 账本水位）。返回 true 不阻断主循环（B4：
// a 永不等待 b）。
//
// **本回调里不得碰引擎历史**：新 Session 装配下它在 Session 锁内同步执行
// （framework `session.ChatStream` 从进函数持锁到出函数），而历史写面
// （AppendHistory/History/ReplaceHistory/ClearHistory）取的就是同一把锁——
// 锁内重入 = 同 goroutine 自锁死：这一轮永不收尾，会话永远停在"运行中"，
// 运行期间收下的排队输入再也不会被提升发送，任务/回合也关不掉
// （2026-09-22 队列提升轮实测现场）。
//
// 因此 b→a 指令**留在待注入队列里不动**，由下一个安全注入点交付——时机仍是
// 既定的"下一次 ChatStream 前"：
//   - 常规轮：startChatFor → injectGoalDirectivesForStart；
//   - 队列提升出来的下一轮：runChat 起手同样注入（提升路径不经过
//     startChatFor，少了这一处裁决就会晚一整轮）。
//
// 可见回放不受影响：回合尾 publishPendingGoalDirectivesFor 用非消费的
// PeekDirectives，裁决照样在产出它的那一回合就可见。
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
	// 返回值不在此处上报：聊天回合本身已成功，治理失败不能把它变成用户可见的聊天
	// 错误；失败原因由协调器登记进只读视图（GoalGovernanceView.RoundError），
	// 面板与 TUI 据此显示「本轮治理未完成」，不靠前端墙钟猜。
	_ = coordinator.AdvanceAfterChat(ctx, sessionID, service.goalTurnWorkSummary(sessionID))
	// "干完就走人"：这一轮把目标收口了，团队就离场（判定见 dismissTeamWhenGoalClosed）。
	service.dismissTeamWhenGoalClosed(sessionID)
}

// RunSeatRound 是座位循环的**执行侧**（seelebridge 的 teamwork.SeatRoundRunner，
// 组合根经 Runtime.SetSeatRoundRunner 注入）：作业执行体在自己的 goroutine 上调它，
// 会话归属与工作正文一律来自**载荷**——作业的执行 ctx 是 jobs.Manager 从
// Background 派生的，不带原调用会话与 detail（这也是 SeatRequest 要带
// SessionID/Detail 的原因）。
//
// 它复用**同一份** goalCoordinator.runSeatRound（驱动唯一化，D4）：作业里跑的座位
// 循环与未装配作业面时的同步循环是同一段正文，不存在"同步一份 + 作业里再一份"。
func (service *Service) RunSeatRound(ctx context.Context, sessionID, detail string, note func(string)) error {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return err
	}
	if err := coordinator.runSeatRound(ctx, sessionID, detail); err != nil {
		return err
	}
	// 作业输出文件里留一行可读结论（作业行的 Summaries/输出面据此有内容，
	// 而不是一个"什么都没发生"的空作业）。治理结论本身在只读治理视图里，
	// 这里不复制第二份裁决口径。
	if note != nil {
		if view := coordinator.GoalGovernanceViewFor(sessionID); view != nil {
			note(fmt.Sprintf("goal 座位循环完成：round=%d seat=%s\n", view.Round, view.CurrentSeat))
		}
	}
	return nil
}

// dismissTeamWhenGoalClosed 让"干完就走人"成立：目标收口（栈里没有 active goal）
// 之后，本会话的在编团队离场（删注册表 + 清顺序）。
//
// 判定用"栈里还有没有 active goal"而不是"是谁召唤的"：召唤与 goal 自动装配写的是
// 同一份团队事实（同一个收口），离场也必须只有一条判据——否则 goal-a2a 自动装配的
// 团队会永远留在会话里，而 `@` 召唤的会走，同一件事两种行为。
//
// 幂等且廉价失败：没有在编团队时只是一次注册表读；离场失败只记日志（下一次收口或
// 下一次装配会再次对齐），不把治理回合的收尾变成错误路径。
func (service *Service) dismissTeamWhenGoalClosed(sessionID string) {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	status, err := service.GoalStatusFor(sessionID)
	if err != nil || status.Active != nil {
		return
	}
	view, err := service.agentTeamRawView(sessionID)
	if err != nil || !view.Configured {
		return
	}
	if err := service.DismissAgentTeam(sessionID); err != nil {
		log.Printf("[goal] 团队离场失败（session=%s）：%v", sessionID, err)
	}
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
// (主会话, team_id, role_name) 派生，与 goal 回合记录器写入 draft 的会话号同源。
// 未装配 AgentTeam（旧会话/测试桩）时返回空——不伪造角色会话号。
func (service *Service) advisorRoleSessionID(sessionID string) string {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	view, err := service.AgentTeamView(sessionID)
	if err != nil || !view.Configured {
		return ""
	}
	return agentteam.RoleSessionID(sessionID, view.TeamID, RoleNameTL)
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

// errGoalDefinitionTLOnly 是 **agent 工具面**尝试改 goal 定义时的拒绝错误。
var errGoalDefinitionTLOnly = errors.New("goal: goal 定义的修改只有 ADVISOR(TL) 裁决侧可发起")

// authorizeAgentGoalMutation 判定"agent 工具面（EXEC / 员工 / 子代理）"是否可以做这次
// goal 变更；nil = 放行。
//
// 权限口径（2026-09-29 定）：**goal 的修改与取消只有 TL（ADVISOR）裁决侧有资格**。
//
//	动作                            | agent 工具面                      | TL 裁决侧 | 人类/运维面（headless RPC）
//	追加进度（progress_*）          | 允许                              | —         | 允许
//	改定义（标题/正文/完成条件/范围）| **拒绝**（本函数）                 | 允许      | 允许
//	取消（finish/abort）            | 无此工具；只有 goal_propose_finish（提议）→ TL 裁决 → 才收口 | 允许（verdict_done / 逃生 AbortOnEscape） | 允许（显式人工操作）
//
// 为什么不让 agent 直接改定义：改定义 = 在被审查的目标上单方面换掉验收标准，ADVISOR
// 的评审依据当场失效（"回合期间 goal 被改"的 B 语义正是这件事的兜底；这里是源头收口）。
// 为什么不拦人类/运维面：人不是 agent——headless 的 goal_update/goal_finish/goal_abort
// 是工作台与运维通道，拦掉它等于把"用户无法取消自己的目标"当成安全，且环逃生
// （AbortOnEscape）也必须保留一条不过 gate 的收口口。
func authorizeAgentGoalMutation(request goaldomain.UpdateRequest) error {
	if !request.ChangesDefinition() {
		return nil
	}
	return fmt.Errorf("%w：goal_update 在 agent 工具面上只接受 progress_kind/progress_content（用它汇报进度即可）", errGoalDefinitionTLOnly)
}

// goalUpdateHandler 是 goal_update 工具 handler（agent 工具面：权限收口见
// authorizeAgentGoalMutation；人类/运维面的显式会话 API 是 GoalUpdateFor）。
func (service *Service) goalUpdateHandler(ctx context.Context, argsJSON string) (string, error) {
	var request goaldomain.UpdateRequest
	if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
		return "", fmt.Errorf("goal_update: invalid arguments: %w", err)
	}
	if err := authorizeAgentGoalMutation(request); err != nil {
		return "", err
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
