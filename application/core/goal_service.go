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
//  2. **goal 的评估链不依赖它**：终态 gate 在任何装配状态下都成立（b 回合由
//     `Supervisor` 带自己的 ADVISOR 上下文跑，不需要团队装配）。
//     团队是"会话里谁在编"这件产品事实的增量，该由用户/leader 决定
//     （leader-worker 目标态里由 team plan 决定）。
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

// GoalDoneFor 按显式会话**直接收口**栈顶 goal（goal_done 工具路径）。
//
// 口径（2026-10-02 用户裁决 B）：main agent 就是 TL 的角色，它可以自己拍板收口——不送
// 终态 gate、不产生提议（领域侧见 goalCoordinator.FinishDirect）。teammate 子会话仍禁：
// goal 工具族对 subagent 整族不可见（seelebridge/tools/policy.go isGoalTool）。
//
// 收口后的收尾与提议路径同款：刷新运行态投影 + 让"干完就走人"成立（栈里没有 active
// goal 时在编团队离场）。目标看板不在这里封板——它跟着审计流水走（goal.finish /
// goal.abort 由 closedInfo 从终态审计反解），多一条收口路径就是两份事实。
func (service *Service) GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.FinishDirect(ctx, sessionID, request, abort)
	if err == nil {
		service.refreshGoalRuntimeProjection(sessionID)
		service.dismissTeamWhenGoalClosed(sessionID)
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
// W3）：不再有可"推进一轮/中断"的治理循环。goal 的收口走终态 gate。

// SetGoalTLEvaluator 注入真实 TL 评估器（组合根：seelebridge 账号面 →
// goal 域 TLEvaluator；首次会话启动前调用）。
func (service *Service) SetGoalTLEvaluator(evaluator goaldomain.TLEvaluator) {
	if service == nil || service.components.goal == nil {
		return
	}
	service.components.goal.setEvaluator(evaluator)
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
// 裁决**不进可见对话**（2026-10-01 口径修正）：ADVISOR 是被调用的 agent，
// 不是对话席位——受信注入是它与 EXEC 之间唯一的交付通道；评审原文留在它自己的
// tl 角色会话里（前端"评审过程"面板），不在聊天区冒充一条发言。
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

// formatDirectiveText 是 b→a 指令的**单行可读形式**：受信注入是它唯一的落地形式
// （2026-10-01 口径修正后不再有可见回放），corr 是唯一的行标识。
func formatDirectiveText(directive goaldomain.TLDirective) string {
	return "[TL 指令 " + directive.Corr + "] " + strings.TrimSpace(directive.Content)
}

// injectGoalDirectives 把 b→a 指令注入引擎受信区——这是 ADVISOR 与 EXEC 之间
// **唯一**的交付通道（ADVISOR 不进可见对话：它是被调用的 agent，不是对话席位）：
//
//   - 注入：以 user 角色写进引擎历史（下一次模型调用就能看到），包在〔〕里
//     与真实用户输入区分。
func (service *Service) injectGoalDirectives(sessionID string, directives []goaldomain.TLDirective) {
	for _, directive := range directives {
		value := "〔" + formatDirectiveText(directive) + "〕"
		service.appendEngineMessage(sessionID, types.Message{Role: "user", Content: &value})
	}
}

// injectGoalDirectivesForStart 在 ChatStream 开始前把 TL 回合产生的指令
// 排空并注入引擎受信区（唯一交付通道；不进可见对话）。
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

// goalAdvanceAfterChat 在 ChatStream 返回后的锁外安全点做一次 goal 收尾记账
// （turn_completed 登记 + 团队环逃生记账）。**席位轮转退场后这里不再跑 ADVISOR
// 回合**：终态判定只在显式入口（goal_propose_finish 的 gate / 审批预筛）。
//
// 返回值不在此处上报：聊天回合本身已成功，收尾记账失败不能把它变成用户可见的
// 聊天错误。
func (service *Service) goalAdvanceAfterChat(ctx context.Context) {
	sessionID := sessionIDFromContext(ctx)
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return
	}
	_ = coordinator.AdvanceAfterChat(ctx, sessionID, service.goalTurnWorkSummary(sessionID))
	// "干完就走人"：这一轮把目标收口了，团队就离场（判定见 dismissTeamWhenGoalClosed）。
	service.dismissTeamWhenGoalClosed(sessionID)
}

// RunSeatRound 已随席位轮转退场删除（2026-10-01 阶段三 W3）：不再有座位作业面。
// 终态 gate 在 goal_propose_finish 的调用栈上同步跑 TL 回合，不经作业。

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

// ADVISOR 的可见回放已删除（2026-10-01 口径修正）：b→a 裁决不再写进可见会话。
// ADVISOR 是被调用的 agent（终态 gate 的评审者），不是对话席位——裁决只走受信
// 注入（injectGoalDirectives）与它自己的 tl 角色会话（goal_team_recorder）。

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
// 权限口径（2026-09-29 定；2026-10-02 用户裁决 B 修"取消"一行）：**goal 的定义修改**
// 只有 TL（ADVISOR）裁决侧有资格；**取消（收口）** 则主代理就够——它在团队里就是 TL 的
// 角色（team_close 与 goal_done 是同一个人拍板），但改验收标准仍然只有裁决侧能做。
//
//	动作                            | agent 工具面                      | TL 裁决侧 | 人类/运维面（headless RPC）
//	追加进度（progress_*）          | 允许                              | —         | 允许
//	改定义（标题/正文/完成条件/范围）| **拒绝**（本函数）                 | 允许      | 允许
//	取消（finish/abort）            | goal_done 直连收口（裁决 B）+ goal_propose_finish 提议 → TL 裁决 | 允许（verdict_done / 逃生 AbortOnEscape） | 允许（显式人工操作）
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

// GoalDoneHandler 是 goal_done 工具 handler（main agent 的真收口面；teammate 看不到这个
// 工具——goal 工具族对 subagent 整族不可见）。
func (service *Service) GoalDoneHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.goalDoneHandler(ctx, argsJSON)
}
