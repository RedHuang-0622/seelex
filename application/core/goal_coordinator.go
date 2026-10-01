package core

// goal_coordinator.go — 会话级 goal 治理协调器（P1 装配）。
//
// 职责：按 sessionID 持有 goal.Controller + Supervisor，
// 提供 Service 侧的 goal 方法面与只读治理视图（GoalGovernanceView）。goal
// 状态/审计落 sessionstore 第五栈（ContextStateStore），未装配会话上下文
// 存储时退化为内存（Store=nil，仅进程内）。本协调器不引入上帝对象：每会话
// 独立 bundle，生命周期随会话；跨会话零共享。

import (
	"context"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// goalCoordinatorDeps 是 goal 协调器装配输入。
type goalCoordinatorDeps struct {
	// StoreFor 返回指定会话的 SessionContextStore（nil = 未装配 → 内存态）。
	StoreFor func(sessionID string) *sessionstore.SessionContextStore
	// Evaluator 是 TL 评估器（nil = TL 未启用，B4 直连收口语义）。
	Evaluator goaldomain.TLEvaluator
	// TLRecorderFor 返回指定会话的 TL 回合记录器（nil = 不记录 b 回合原文）。
	TLRecorderFor func(sessionID string) goaldomain.TLRoundRecorder
	// TeamRuntimeFor 返回指定会话的团队发言调度运行态。
	//
	// 席位轮转退场后（2026-10-01 阶段三 W3），它**只剩一个用途**：环逃生记账
	// （`team.NoteTurn` 按"本轮有没有推进"记轮次，到达上限即收束）。goal 的
	// 驱动不再经过任何座位，因此这里不再有"座位派生"语义。
	TeamRuntimeFor func(sessionID string) *agentteam.Runtime
}

// goalSessionRuntime 是一个会话的 goal 治理 bundle（会话间零共享）。
type goalSessionRuntime struct {
	ctl *goaldomain.Controller
	sup *goaldomain.Supervisor
}

type goalCoordinator struct {
	mu       sync.Mutex
	deps     goalCoordinatorDeps
	sessions map[string]*goalSessionRuntime
}

func newGoalCoordinator(deps goalCoordinatorDeps) *goalCoordinator {
	return &goalCoordinator{
		deps:     deps,
		sessions: make(map[string]*goalSessionRuntime),
	}
}

// bundleFor 返回（需要时创建）指定会话的 goal bundle。创建时若装配了会话
// 上下文存储，则经 ContextStateStore 持久化并 Reload 恢复活栈/审计。
func (g *goalCoordinator) bundleFor(sessionID string) *goalSessionRuntime {
	g.mu.Lock()
	defer g.mu.Unlock()
	if runtime := g.sessions[sessionID]; runtime != nil {
		return runtime
	}
	var store goaldomain.Store
	var audit goaldomain.AuditAccount
	if g.deps.StoreFor != nil {
		if contextStore := g.deps.StoreFor(sessionID); contextStore != nil {
			adapter := goaldomain.NewContextStateStore(contextStore)
			store, audit = adapter, adapter
		}
	}
	controller := goaldomain.NewController(goaldomain.Options{
		Depth: goaldomain.DefaultStackDepth, Store: store, Audit: audit,
	})
	if store != nil {
		if err := controller.Reload(context.Background()); err != nil {
			// 恢复失败不阻塞会话：退回内存态（进程内治理仍可用；持久化
			// 故障由存储层显式报错，不静默吞掉审计语义）。
			controller = goaldomain.NewController(goaldomain.Options{
				Depth: goaldomain.DefaultStackDepth,
			})
		}
	}
	runtime := &goalSessionRuntime{
		ctl: controller,
		sup: goaldomain.NewSupervisor(controller, g.deps.Evaluator, goaldomain.DefaultTechLeaderConfig()),
	}
	// 主会话坐标：ADVISOR 回合的输入要带上"评审哪个工作区"，执行面才能绑定项目根
	// 并给评审者只读工具（没有它，评审只能凭上下文猜，见 runtime_goal_tl.go）。
	runtime.sup.SetSessionID(sessionID)
	if g.deps.TLRecorderFor != nil {
		runtime.sup.SetRoundRecorder(g.deps.TLRecorderFor(sessionID))
	}
	g.sessions[sessionID] = runtime
	return runtime
}

// noteRoundError 已随席位轮转退场删除：不再有"治理回合"，也就没有回合失败可登记。
// 回合失败语义改由显式入口（终态 gate / 审批预筛）自己返回 error。

// Begin 注册并压栈（会话路由）。
func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	runtime := g.bundleFor(sessionID)
	before, _ := runtime.ctl.ActiveGoal()
	record, err := runtime.ctl.Begin(ctx, request)
	if err != nil {
		return nil, err
	}
	// 团队环的逃生结论只属于**上一轮** goal：环停止后 SyncOrder 不会复活它，
	// 新 goal 上线时把逃生记账清零（顺序/成员不动，仍是 lifecycle 那一份事实）。
	// 未装配团队环时 teamRuntimeFor 返回 nil，Reset 对 nil 接收者安全。
	//
	// 幂等 begin（同名返回既有 active）不重置。
	if record != nil && (before == nil || before.ID != record.ID) {
		g.teamRuntimeFor(sessionID).Reset()
	}
	return record, nil
}

// Update 更新栈顶（会话路由）。
func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return g.bundleFor(sessionID).ctl.Update(ctx, request)
}

// ProposeFinish 送终态 gate（TL 缺席时 OutcomeNoTL 直连收口；B4）。
func (g *goalCoordinator) ProposeFinish(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error) {
	return g.bundleFor(sessionID).sup.ProposeFinish(ctx, request)
}

// Notify 登记 a 事件（exec 账本；触发策略见 Supervisor）。
func (g *goalCoordinator) Notify(ctx context.Context, sessionID string, signal goaldomain.TLEvalSignal) error {
	return g.bundleFor(sessionID).sup.Notify(ctx, signal)
}

// Next 已随席位轮转退场删除：回合尾不再自动推进座位循环。终态判定走显式入口
// （goal_propose_finish / 审批预筛）。

// AdvanceAfterChat 在 ChatStream 返回后的回合边界安全点推进一次 goal 收尾记账：
// 登记 turn_completed（exec 账本水位 + 本轮工作正文摘要）与团队环逃生记账。
//
// **席位轮转已退场**（2026-10-01 阶段三 W3）：回合尾**不再**自动跑"exec 让位 →
// advisor 评审"的座位循环。goal 的驱动改为提示词驱动的 leader 派活（见
// plugins/default/goal/SKILL.md），终态判定只发生在显式入口——`goal_propose_finish`
// 的终态 gate（Supervisor 的真实 TL 回合）与审批预筛。这里保留的只有：
//   - turn_completed 信号登记（EXEC 账本水位 + 工作正文摘要）；
//   - 团队环逃生记账（到达轮次上限/连续无进展 → 收口 goal + 归档 b 历史）。
//
// 返回值不再有"治理回合失败"语义（没有回合了）；调用方丢弃它也不影响聊天成功。
func (g *goalCoordinator) AdvanceAfterChat(ctx context.Context, sessionID, detail string) error {
	runtime := g.bundleFor(sessionID)
	if runtime.ctl.Status().Active == nil {
		return nil
	}
	if err := runtime.sup.Notify(ctx, goaldomain.TLEvalSignal{
		Kind: goaldomain.SignalTurnCompleted, Source: "chat_end", Detail: detail,
	}); err != nil {
		return err
	}
	// 逃生记账：团队环按"本轮有没有推进"记一次轮次。到达轮次上限或连续多轮
	// 无进展时，环显式收束。
	//
	// 逃生 = 这一轮 goal 结束：让 Supervisor 收口（goal 落终态 aborted + 归档 b
	// 侧会话历史 + reap peer）。不收口会让 goal 挂在 active 等一个永远不会来的
	// 收口，而且下一个 goal 可能复用上一轮 b 的锚点/帧。
	//
	// detail 只用于"本轮有没有推进"的判定与 b 评审输入；它**不再**驱动任何座位
	// （员工干活走 leader 派发的 worker 作业，正文随作业载荷走）。
	if team := g.teamRuntimeFor(sessionID); team != nil {
		if stopped, reason := team.NoteTurn(strings.TrimSpace(detail) != ""); stopped {
			if _, err := runtime.sup.AbortOnEscape(ctx, reason); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

// teamRuntimeFor 取该会话的团队发言调度运行态（未装配团队环 → nil）。它**只**
// 服务环逃生记账（见 AdvanceAfterChat）。
func (g *goalCoordinator) teamRuntimeFor(sessionID string) *agentteam.Runtime {
	if g.deps.TeamRuntimeFor == nil {
		return nil
	}
	return g.deps.TeamRuntimeFor(sessionID)
}

// 席位轮转退场（2026-10-01 阶段三 W3）：治理循环上限、座位派生、座位作业面
// （newGovernor / seatsFor / seatPlan / SeatJobs / RoleSeat / runSeatRound）已
// 一并删除。goal 的驱动不再经过任何座位。

// 座位派生（RoleSeat / seatsFor / seatPlan.seats）已随席位轮转退场删除。

// 座位作业面（SeatJobs / SeatJobsAssembled / SeatJobOutcome）与退化座位匹配
// （seatsFromOrder / orderSeats / teamOrderFor / teamRoleSeat）已随席位轮转退场删除。

// Break 已随席位轮转退场删除：没有治理循环可中断。goal 的收口走终态 gate。

// setEvaluator 装配/替换 TL 评估器：更新后续会话 bundle 构造输入，并为已
// 存在的会话重建 Supervisor（评审会话从下一次显式入口重新 bind）。
// 供组合根在首次会话启动前注入真实 TLEvaluator。
func (g *goalCoordinator) setEvaluator(evaluator goaldomain.TLEvaluator) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deps.Evaluator = evaluator
	for sessionID, runtime := range g.sessions {
		runtime.sup = goaldomain.NewSupervisor(runtime.ctl, evaluator, goaldomain.DefaultTechLeaderConfig())
		// 重建 Supervisor 必须把主会话坐标重新写回：换评估器不该把"评审哪个工作区"
		// 丢掉（丢了 = ADVISOR 退回无工具评审，静默降级）。
		runtime.sup.SetSessionID(sessionID)
	}
}

// StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
func (g *goalCoordinator) StatusFor(sessionID string) goaldomain.StatusView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return goaldomain.StatusView{}
	}
	return runtime.ctl.Status()
}

// DrainDirectives 排空该会话 b→a 指令队列（ChatStream 回合边界注入）。
func (g *goalCoordinator) DrainDirectives(sessionID string) []goaldomain.TLDirective {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.sup.Mailbox().DrainDirectives()
}

// PeekDirectives 读取该会话待注入的 b→a 指令（不消费）：回合结束时把刚产出的
// 裁决立刻回放进可见会话用。队列本身留给下一次 ChatStream 前的受信注入消费。
func (g *goalCoordinator) PeekDirectives(sessionID string) []goaldomain.TLDirective {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.sup.Mailbox().PeekDirectives()
}

// goalStackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
// = active 那一帧）。
//
// 只读：不写回任何 goal 状态，也不新增第二份"栈事实"——事实仍是 Controller 的
// LIFO 栈。每帧带自己的标题/陈述/状态/验收/非目标范围/打点，工作台据此渲染看板
// 卡片与"点开看详情"的属性表。
//
// 打点给两份：Progress = 最近 frameProgressLimit 条（卡片一行摘要），
// ProgressAll = 这一帧保留的全部（详情面的完整流水）。
func goalStackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView {
	if len(stack) == 0 {
		return nil
	}
	const frameProgressLimit = 3
	frames := make([]dto.GoalFrameView, 0, len(stack))
	for index, record := range stack {
		if record == nil {
			continue
		}
		frame := dto.GoalFrameView{
			ID:         record.ID,
			Title:      record.Title,
			Statement:  record.Statement,
			Status:     string(record.Status),
			Active:     index == len(stack)-1,
			Acceptance: append([]string(nil), record.Acceptance...),
			OutOfScope: append([]string(nil), record.OutOfScope...),
			CreatedAt:  record.CreatedAt,
			UpdatedAt:  record.UpdatedAt,
		}
		for _, item := range record.Progress {
			view := dto.GoalProgressView{At: item.At, Kind: string(item.Kind), Content: item.Content}
			frame.ProgressAll = append(frame.ProgressAll, view)
		}
		progress := record.Progress
		if len(progress) > frameProgressLimit {
			progress = progress[len(progress)-frameProgressLimit:]
		}
		for _, item := range progress {
			frame.Progress = append(frame.Progress, dto.GoalProgressView{
				At: item.At, Kind: string(item.Kind), Content: item.Content,
			})
		}
		frames = append(frames, frame)
	}
	return frames
}

// ADVISOR 的可见回放记账（DirectivePublished / MarkDirectivePublished /
// NoteInjected / TakeInjected）已删除（2026-10-01 口径修正）：裁决不再回放进可见
// 会话，"待回放/已回放"这份账本随之失去唯一消费者。裁决只走受信注入与它自己的
// tl 角色会话。

// goalStepViews 把 goal 域的评审过程步骤投影成只读 DTO（nil 进 → nil 出，
// 面板不显示空壳）。
func goalStepViews(steps []goaldomain.TLStep) []dto.GoalStepView {
	if len(steps) == 0 {
		return nil
	}
	views := make([]dto.GoalStepView, 0, len(steps))
	for _, step := range steps {
		views = append(views, dto.GoalStepView{
			Kind: step.Kind, Turn: step.Turn, Name: step.Name,
			Args: step.Args, Result: step.Result, Err: step.Err, At: step.At,
		})
	}
	return views
}

// GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal → nil，前端隐藏）。
//
// 席位轮转退场后，视图不再有"轮次/座次/断环"这类循环概念：它只投影 goal
// **看板**（活动栈逐帧 + 状态 + 最近裁决）。评审过程（round_steps / in_flight /
// peer_state）仍在——但只在终态 gate 或审批预筛跑真实 TL 回合时短暂有值。
func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return nil
	}
	status := runtime.ctl.Status()
	if status.Active == nil {
		return &dto.GoalGovernanceView{Active: false}
	}
	peer := runtime.sup.Snapshot()
	view := &dto.GoalGovernanceView{
		Active:    true,
		GoalID:    status.Active.ID,
		Title:     status.Active.Title,
		Status:    string(status.Active.Status),
		PeerState: string(peer.Peer),
		// 进行中的 ADVISOR 正文（只读快照）：回合结束为空。前端据此在评审期间
		// 轮询快照，把"评审在写什么"及时渲染出来。
		InFlight:      peer.InFlight,
		InFlightChars: peer.InFlightChars,
		RoundSteps:    goalStepViews(peer.RoundSteps),
	}
	// 每帧的只读投影：工作台按**活动栈**分块展示（栈顶=当前目标，栈下=被嵌套
	// 压栈而暂停的目标）。栈只有一份事实（Controller 的 LIFO 栈），这里只读。
	view.Stack = goalStackFrames(status.Stack)
	if rounds := peer.Rounds; len(rounds) > 0 {
		view.LastDirective = rounds[len(rounds)-1].Summary
	}
	return view
}
