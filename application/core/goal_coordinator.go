package core

// goal_coordinator.go — 会话级 goal 治理协调器（P1 装配）。
//
// 职责：按 sessionID 持有 goal.Controller + Supervisor（+ 惰性 Governor），
// 提供 Service 侧的 goal 方法面与只读治理视图（GoalGovernanceView）。goal
// 状态/审计落 sessionstore 第五栈（ContextStateStore），未装配会话上下文
// 存储时退化为内存（Store=nil，仅进程内）。本协调器不引入上帝对象：每会话
// 独立 bundle，生命周期随会话；跨会话零共享。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/application/core/govern"
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
	// MaxRounds 是治理循环轮次护栏（≤0 = 不设上限，由裁决/Break 收束）。
	MaxRounds int
	// TeamRuntimeFor 返回指定会话的团队发言调度运行态（链表顺序 + 逃生记账）。
	// 治理循环据此决定"哪些座位真的存在"（顺序里没有 main/tl 就不该凭空长出
	// 座位）；nil 或返回 nil 时退回内置的 EXEC+ADVISOR 双座位。
	TeamRuntimeFor func(sessionID string) *agentteam.Runtime
	// RoleSeatsFor 返回该会话团队的角色座位来源（按发言链顺序）。座位按 **角色
	// kind** 派生（见 seatPlan.seats）：没有它时退回按链表顺序 + 角色名字面量匹配
	// 的老路径——老宿主/桩不因新增读面而炸。
	RoleSeatsFor func(sessionID string) []RoleSeat
	// RoleTurnFor 返回该会话的**员工执行面**（nil = 未装配：agent 角色只占发言位，
	// 不推进治理循环）。V 模型团队循环（pm → exec → test case）落地时，实现方在
	// 这里注入：每个具备执行面的员工角色因此获得一个真正干活的座位。
	RoleTurnFor func(sessionID string) RoleTurnRunner
}

// goalSessionRuntime 是一个会话的 goal 治理 bundle（会话间零共享）。
type goalSessionRuntime struct {
	ctl *goaldomain.Controller
	sup *goaldomain.Supervisor
	gov govern.Governor // 惰性装配（首次 Next）
}

type goalCoordinator struct {
	mu       sync.Mutex
	deps     goalCoordinatorDeps
	sessions map[string]*goalSessionRuntime

	// roundError 是各会话**上一轮治理推进失败的原因**（空 = 无失败）：goal 保持
	// active 是回合失败时的安全默认，但失败原因必须可见，否则面板只能靠墙钟猜。
	roundError map[string]string
	// injections 是"已注入引擎受信区、待可见回放"的指令（回合尾回放，见
	// Service.injectGoalDirectivesFor）。
	injections map[string][]goaldomain.TLDirective
	// published 记录"已回放进可见会话"的指令 corr（每会话一集）：指令产出的那一
	// 回合就要可见（Service.publishPendingGoalDirectivesFor），而下一次回合的
	// 常规回放不能把它再写一遍（corr 幂等）。
	published map[string]map[string]bool
}

func newGoalCoordinator(deps goalCoordinatorDeps) *goalCoordinator {
	return &goalCoordinator{
		deps:       deps,
		sessions:   make(map[string]*goalSessionRuntime),
		roundError: make(map[string]string),
		injections: make(map[string][]goaldomain.TLDirective),
		published:  make(map[string]map[string]bool),
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

// noteRoundError 登记（或清除）该会话上一轮治理推进的失败原因。
//
// ErrTLDisabled 是配置态（没装配 TL 评估器），不是回合故障，按"无失败"处理。
func (g *goalCoordinator) noteRoundError(sessionID string, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err == nil || errors.Is(err, goaldomain.ErrTLDisabled) {
		delete(g.roundError, sessionID)
		return
	}
	g.roundError[sessionID] = err.Error()
}

// Begin 注册并压栈（会话路由）。
func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	runtime := g.bundleFor(sessionID)
	before, _ := runtime.ctl.ActiveGoal()
	record, err := runtime.ctl.Begin(ctx, request)
	if err != nil {
		return nil, err
	}
	// 新 goal = 新的治理循环：上一轮 goal 收口/断环留下的 governor 不能沿用到
	// 新目标上——已断环的 governor 会让新 goal 再也等不到 ADVISOR 回合（治理
	// 面板恒 0 轮、loop 不动）。幂等 begin（同名返回既有 active）不重置。
	if record != nil && (before == nil || before.ID != record.ID) {
		runtime.gov = nil
		// 团队环的逃生结论同样只属于**上一轮** goal：环停止后 SyncOrder 不会
		// 复活它，而 AdvanceAfterChat 每次都会先看到 stopped=true 并立刻
		// Break 新装配的 governor——ADVISOR 因此彻底静默。新 goal 上线时把
		// 逃生记账清零（顺序/成员不动，仍是 lifecycle 那一份事实）。
		// 未装配团队环时 teamRuntimeFor 返回 nil，Reset 对 nil 接收者安全。
		g.teamRuntimeFor(sessionID).Reset()
	}
	// 新 goal 不继承上一轮的治理失败记录（面板不能拿着旧错误解释新目标）。
	if record != nil && (before == nil || before.ID != record.ID) {
		g.noteRoundError(sessionID, nil)
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

// Next 推进治理循环一轮（惰性装配座位；返回 false = 收束）。
func (g *goalCoordinator) Next(ctx context.Context, sessionID string) (bool, error) {
	runtime := g.bundleFor(sessionID)
	if runtime.gov == nil {
		runtime.gov = g.newGovernor(sessionID, runtime)
	}
	more, err := runtime.gov.Next(ctx)
	g.noteRoundError(sessionID, err)
	return more, err
}

// AdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进一次治理：登记
// turn_completed（exec 账本水位 + 本轮工作正文摘要），若 TL 已启用则运行一轮
// Governor（exec 让位 → advisor 真实 TL 回合）。TL 缺席/未启用按 B4 忽略，不阻塞。
// detail 为空 = 本轮无可摘要产出（信号仍登记，水位不跳）。
//
// 本方法是治理回合失败的唯一登记点：goal 保持 active 是安全默认，但失败原因必须
// 进只读视图（GoalGovernanceView.RoundError），否则调用方丢弃错误后，面板只剩墙钟
// 可猜——把「合法空闲」和「真卡住」印成同一句话。
func (g *goalCoordinator) AdvanceAfterChat(ctx context.Context, sessionID, detail string) error {
	err := g.advanceAfterChat(ctx, sessionID, detail)
	g.noteRoundError(sessionID, err)
	return err
}

func (g *goalCoordinator) advanceAfterChat(ctx context.Context, sessionID, detail string) error {
	runtime := g.bundleFor(sessionID)
	if runtime.ctl.Status().Active == nil {
		return nil
	}
	if err := runtime.sup.Notify(ctx, goaldomain.TLEvalSignal{
		Kind: goaldomain.SignalTurnCompleted, Source: "chat_end", Detail: detail,
	}); err != nil {
		return err
	}
	if !runtime.sup.Enabled() {
		return nil
	}
	if runtime.gov == nil {
		runtime.gov = g.newGovernor(sessionID, runtime)
	}
	// 逃生记账：团队环按"本轮有没有推进"记一次轮次。到达轮次上限或连续多轮
	// 无进展时，环显式收束。
	//
	// 逃生 = 这一轮 goal 结束：先 Break 掉治理循环，再让 Supervisor 收口
	// （goal 落终态 aborted + 归档 b 侧会话历史 + reap peer）。只 Break 不收口
	// 会让 goal 挂在 active 等一个永远不会来的 ADVISOR 回合（面板恒 0 轮、用户
	// 看不到收口），而且下一个 goal 可能复用上一轮 b 的锚点/帧。
	if team := g.teamRuntimeFor(sessionID); team != nil {
		// 逃生记账：团队环按"本轮有没有推进"记一次轮次。
		//
		// 这里**不再**把本轮正文摘要写进 team work 前缀——前缀的作者是主会话上下文
		// （含主会话 draft）的只读装配（见 Service.noteTeamWorkPrefix）：TL 的对话
		// 记录就是 engine loop 写出的行，用治理域的回合摘要当前缀会当场变成第三套
		// 口径。detail 只用于它本来的两个用途：b（ADVISOR）评审输入与员工座位本轮
		// 的输入。
		if stopped, reason := team.NoteTurn(strings.TrimSpace(detail) != ""); stopped {
			runtime.gov.Break(reason)
			if _, err := runtime.sup.AbortOnEscape(ctx, reason); err != nil {
				return err
			}
			return nil
		}
	}
	// Governor.Next 每次只推进一个座位；推进一整轮（员工 → 评审）需要执行到
	// Round 递增或断环为止。
	//
	// 上限按**实际座位数**取，不能写死：试水形态是 2 座（EXEC + ADVISOR），
	// 员工执行面接入后是 pm/exec/test + ADVISOR = 4 座以上——写死 2 会让多座团队
	// 永远走不完一轮（Round 永不递增 → 轮次上限形同虚设、每轮只推进前两个座位）。
	roundBefore := runtime.gov.Round()
	attempts := len(runtime.gov.Seats()) + 1
	if attempts < 3 {
		attempts = 3
	}
	// 本轮工作正文经 ctx 透传到员工座位的执行面：座位在装配期构造、不持有
	// "这一轮发生了什么"，而员工回合必须拿到本轮 detail 才有活可干（否则只能
	// 凭空猜，等于用一个空输入跑一次模型调用）。ctx 是唯一不引入新共享状态
	// 的通道——并发会话各自持自己的 ctx。
	ctx = withRoleTurnInput(ctx, detail)
	for attempt := 0; attempt < attempts; attempt++ {
		more, err := runtime.gov.Next(ctx)
		if err != nil {
			if errors.Is(err, goaldomain.ErrTLDisabled) {
				return nil
			}
			return err
		}
		if !more || runtime.gov.Round() > roundBefore {
			break
		}
	}
	return nil
}

// teamRuntimeFor 取该会话的团队发言调度运行态（未装配团队环 → nil）。
func (g *goalCoordinator) teamRuntimeFor(sessionID string) *agentteam.Runtime {
	if g.deps.TeamRuntimeFor == nil {
		return nil
	}
	return g.deps.TeamRuntimeFor(sessionID)
}

// defaultGoalLoopMaxRounds 是治理循环的**默认轮次上限**（逃生路径的最后一道
// 兜底）：deps.MaxRounds 未配置（0）时用它，避免"没人设置 = 无限循环"。
// 显式传负数 = 主动放弃轮次上限（只保留裁决/Break 收束，属高级用法）。
const defaultGoalLoopMaxRounds = 24

// goalLoopRoundLimit 把配置值解析成实际生效的轮次上限。
func goalLoopRoundLimit(configured int) int {
	switch {
	case configured > 0:
		return configured
	case configured < 0:
		return 0 // 0 = 治理层不设上限（显式选择）
	default:
		return defaultGoalLoopMaxRounds
	}
}

// newGovernor 装配治理循环座位。座位的**存在性**由团队工作顺序（链表）决定：
// 顺序里有 main 才有 EXEC 座位、有 tl 才有 ADVISOR 座位；顺序里没有的座位不
// 凭空长出来。顺序完全对不上（或宿主未装配团队环）时退回内置双座位，保证
// goal 治理在未装配 AgentTeam 的宿主上照常工作。
//
// 座位名保持 "exec-a" / "advisor-b"（历史口径：headless 快照与巡检面依赖）。
// EXEC 由外部 ChatStream 驱动，座位只让位。
func (g *goalCoordinator) newGovernor(sessionID string, runtime *goalSessionRuntime) govern.Governor {
	execAct := func(context.Context) (govern.TurnAction, error) {
		return govern.TurnAction{}, nil
	}
	limit := goalLoopRoundLimit(g.deps.MaxRounds)
	seats := g.seatsFor(sessionID, runtime.sup, execAct)
	if len(seats) == 0 {
		return goaldomain.NewTurnGovernorForDSA2A("exec-a", execAct, runtime.sup, limit)
	}
	// EXEC 必须先于 ADVISOR：治理语义是"执行让位 → 评审"，顺序里两座都在时
	// 按这个固定相对次序排（座位的存在与否仍由链表决定）。
	return govern.NewTurnGovernor(orderSeats(seats), limit)
}

// RoleSeat 是一个角色座位的来源事实（注册表 + 成员表的最小投影，按发言链顺序）。
// 带 RoleSessionID/ToolsPolicy/PermissionGroups 是因为"员工执行面"的座位需要它们：
// 执行面要按角色会话跑回合、按权责口径与逐格权限落地主体。
type RoleSeat struct {
	RoleName      string
	RoleKind      dto.RoleKind
	RoleSessionID string
	ToolsPolicy   string
	// PermissionGroups 是该角色**逐格装配**的权限（路由组 → 位，空 = 按档位派生）。
	PermissionGroups map[string]uint8
}

// seatsFor 按团队注册表的**角色 kind** 派生治理座位。
//
// 为什么必须按 kind 而不是角色名：旧实现用字面量比较（"main" / "tl"）决定谁有
// 座位，于是 TL 角色一旦改名（用户改名，或自定义预设给了别的名字），ADVISOR
// 座位就直接消失——治理循环退化成只有一个 EXEC 座位：ADVISOR 被彻底噤声，
// 而且不报错，只是"永远不评估"。
//
// 座位来源优先级：
//  1. 注册表按发言链顺序给出的角色座位（RoleSeatsFor）——kind 决定座位；
//  2. 退化路径：读不到注册表（老宿主/桩）时按链表顺序 + 旧的字面量规则匹配，
//     保证这类宿主不会因为新增读面而炸。
func (g *goalCoordinator) seatsFor(sessionID string, supervisor *goaldomain.Supervisor, execAct func(context.Context) (govern.TurnAction, error)) []govern.Seat {
	if g.deps.RoleSeatsFor != nil {
		if specs := g.deps.RoleSeatsFor(sessionID); len(specs) > 0 {
			plan := seatPlan{
				SessionID:  sessionID,
				Seats:      specs,
				Supervisor: supervisor,
				ExecAct:    execAct,
				Runner:     g.roleTurnRunnerFor(sessionID),
			}
			if seats := plan.seats(); len(seats) > 0 {
				return seats
			}
		}
	}
	return seatsFromOrder(g.teamOrderFor(sessionID), supervisor, execAct)
}

// roleTurnRunnerFor 取该会话的员工执行面（未装配 → nil = 试水形态）。
func (g *goalCoordinator) roleTurnRunnerFor(sessionID string) RoleTurnRunner {
	if g.deps.RoleTurnFor == nil {
		return nil
	}
	runner := g.deps.RoleTurnFor(sessionID)
	if runner == nil {
		return nil
	}
	return runner
}

// seatPlan 是座位派生的一份输入快照（纯数据：单测可直接构造它钉住映射）。
type seatPlan struct {
	SessionID  string
	Seats      []RoleSeat
	Supervisor *goaldomain.Supervisor
	ExecAct    func(context.Context) (govern.TurnAction, error)
	Runner     RoleTurnRunner
}

// seats 按角色 kind 派生座位（纯函数，便于单测钉住"改名不丢座位"与
// "谁只占发言位、谁有执行面"）。
//
// 映射（V 模型团队循环口径：pm → exec → test case，末尾评审）：
//   - main      → EXEC 座位：用户/主代理的会谈本身就是执行面（外部驱动），
//     座位只让位、不代跑，避免同一份工作在两条路上各跑一次
//   - techlead  → ADVISOR 座位（评审者）
//   - agent     → **员工执行面座位**：装配了 RoleTurnRunner 才有座位，每一轮真的
//     跑一次该角色带工具的回合；未装配时**不给座位**——没有执行面的员工占着座位，
//     只会让治理循环空转等一个永远不会发生的回合（这也正是"ADVISOR 被噤声"的
//     反面：宁可少一座，不要假一座）
//   - timer/user→ 无治理座位（在环里发言，不推进治理循环）
//
// 关于 kind：govern.AgentKind 只有 exec/advisor 两种，员工的座位复用
// AgentKindExec 表达"做工的座位"。角色身份由 RoleName/座位名表达，不用 kind 去
// 区分 pm/exec/test——否则每加一个角色都要改治理抽象。
func (plan seatPlan) seats() []govern.Seat {
	seats := make([]govern.Seat, 0, len(plan.Seats))
	for index, seat := range plan.Seats {
		switch seat.RoleKind {
		case dto.RoleKindMain:
			seats = append(seats, teamRoleSeat{name: "exec-a", kind: govern.AgentKindExec, act: plan.ExecAct})
		case dto.RoleKindTechlead:
			seats = append(seats, goaldomain.NewAdvisorSeat(plan.Supervisor, ""))
		case dto.RoleKindAgent:
			if plan.Runner == nil {
				continue
			}
			seats = append(seats, newRoleTurnSeat(seat, index, plan.SessionID, plan.Runner))
		}
	}
	return seats
}

// roleTurnSeat 是**员工执行面**座位：每次 Act 跑该角色的一轮带工具回合。
//
// 与 EXEC/ADVISOR 座位的区别：它的动作不是"让位"，而是真的一次员工回合——这正是
// V 模型团队循环里 pm/exec/test case 各自做工的位置。
type roleTurnSeat struct {
	name    string
	request RoleTurnRequest
	runner  RoleTurnRunner
}

func newRoleTurnSeat(seat RoleSeat, orderIndex int, sessionID string, runner RoleTurnRunner) roleTurnSeat {
	name := strings.TrimSpace(seat.RoleName)
	request := RoleTurnRequest{
		SessionID:     sessionID,
		RoleName:      name,
		RoleSessionID: strings.TrimSpace(seat.RoleSessionID),
		ToolsPolicy:   seat.ToolsPolicy,
		OrderIndex:    orderIndex,
	}
	if len(seat.PermissionGroups) > 0 {
		request.PermissionGroups = make(map[string]uint8, len(seat.PermissionGroups))
		for group, bits := range seat.PermissionGroups {
			request.PermissionGroups[group] = bits
		}
	}
	return roleTurnSeat{name: name, runner: runner, request: request}
}

func (seat roleTurnSeat) Name() string           { return seat.name }
func (seat roleTurnSeat) Kind() govern.AgentKind { return govern.AgentKindExec }

// roleTurnInputCtxKey 携带"本轮工作正文"：座位在装配期构造、不持有这一轮的
// detail，而员工回合没有输入就等于用一个空 prompt 烧一次模型调用。用 ctx 而不是
// 座位字段，是因为同一批座位对象会被多轮复用、且并发会话各自持自己的 ctx。
type roleTurnInputCtxKey struct{}

func withRoleTurnInput(ctx context.Context, detail string) context.Context {
	if strings.TrimSpace(detail) == "" {
		return ctx
	}
	return context.WithValue(ctx, roleTurnInputCtxKey{}, detail)
}

func roleTurnInputFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	detail, _ := ctx.Value(roleTurnInputCtxKey{}).(string)
	return detail
}

// Act 跑一轮员工回合。错误向上抛（治理循环透传）：执行面出错不能吞成"没产出"，
// 否则环会把它记成无进展并逃生，掩盖真正的故障。
func (seat roleTurnSeat) Act(ctx context.Context) (govern.TurnAction, error) {
	request := seat.request
	if strings.TrimSpace(request.Input) == "" {
		request.Input = roleTurnInputFromContext(ctx)
	}
	outcome, err := seat.runner.RunRoleTurn(ctx, request)
	if err != nil {
		return govern.TurnAction{}, err
	}
	return govern.TurnAction{Note: roleTurnNote(outcome)}, nil
}

// roleTurnNote 把一轮员工回合的结论落成面板可见的一句话：没跑起来和跑了没产出
// 都要能被看见（否则"员工座位一直是空的"会被误读成"员工干完了"）。
func roleTurnNote(outcome RoleTurnOutcome) string {
	note := strings.TrimSpace(outcome.Note)
	switch {
	case !outcome.Ran:
		note = strings.TrimSpace(note + "（员工未跑：无输入或被权限拦下）")
	case !outcome.Progress:
		note = strings.TrimSpace(note + "（本轮无产出）")
	}
	return strings.TrimSpace(note)
}

// RoleTurnRequest 描述"跑一个员工角色的一轮"所需的全部上下文。
//
// 执行面（RoleTurnRunner 的实现方）拿到它就应当：在该角色的会话上跑**一次带工具的
// 回合**，并保证主体类按 ToolsPolicy 落地（seelebridge 侧调 tools.WithEmployeeSubjectClass）。
// 把 ToolsPolicy 与 RoleSessionID 一起交给执行面，是为了让"角色权责"和"角色回合"
// 在同一条路上成立：权限拦截不是事后补的解析，而是该回合的起点条件。
type RoleTurnRequest struct {
	SessionID     string // 主会话（团队注册表/环的属主）
	RoleName      string // 角色名（V 模型里如 pm / exec / test_case）
	RoleSessionID string // 角色会话（员工自己的会话）
	ToolsPolicy   string // 该角色的权责口径（readonly / readwrite / full）
	OrderIndex    int    // 在发言链里的位次（0 起）
	// PermissionGroups 是该角色**逐格装配**的权限（路由组 → 位）。非空时优先于
	// ToolsPolicy：执行体开角色会话时按它落地主体条目，起手按它放主体进 ctx。
	PermissionGroups map[string]uint8
	// Input 是本轮该角色拿到的"工作正文"（AdvanceAfterChat 的 detail，经 ctx
	// 透传到座位）。为空时执行体只跑一次"按角色设定继续"的回合，不凭空补全。
	Input string
}

// RoleTurnOutcome 是一轮员工回合的结论：治理循环用它写面板（Note）并喂逃生记账
// （Progress=false 的轮次会被环的 no_progress 口径计入）。
type RoleTurnOutcome struct {
	Ran      bool   // 真的跑了模型回合（false = 该角色这轮没动，如无输入/被权限拦下）
	Progress bool   // 这一轮是否推进了目标
	Note     string // 一句话摘要（进 TurnAction.Note）
}

// RoleTurnRunner 是**员工执行面**（V 模型团队循环里"员工真的干活"的那一面）。
//
// 它由 seelebridge 侧实现（模型会话 + 工具面 + 角色存储），经 RoleTurnFor 注入；
// application/core 只声明契约、只决定"谁有座位、谁先谁后"。
// 未装配（nil）= 当前试水形态：员工角色只在环里占发言位，不推进治理循环。
type RoleTurnRunner interface {
	RunRoleTurn(ctx context.Context, request RoleTurnRequest) (RoleTurnOutcome, error)
}

// seatsFromOrder 是退化路径：只有链表顺序（角色名）时按名字匹配。
// 它存在是为了让"没有注册表读面"的老宿主继续可用，不是为了兼容改名——
// 装配了注册表读面的会话一律走 seatsFromSpecs。
func seatsFromOrder(order []string, supervisor *goaldomain.Supervisor, execAct func(context.Context) (govern.TurnAction, error)) []govern.Seat {
	seats := make([]govern.Seat, 0, 2)
	for _, roleName := range order {
		switch {
		case roleName == string(dto.RoleKindMain):
			seats = append(seats, teamRoleSeat{name: "exec-a", kind: govern.AgentKindExec, act: execAct})
		case roleName == agentteam.RoleTechlead || roleName == string(dto.RoleKindTechlead):
			seats = append(seats, goaldomain.NewAdvisorSeat(supervisor, ""))
		}
	}
	return seats
}

// orderSeats 把座位按 EXEC → ADVISOR 归位（同 kind 保持链表次序）。
func orderSeats(seats []govern.Seat) []govern.Seat {
	out := make([]govern.Seat, 0, len(seats))
	for _, seat := range seats {
		if seat.Kind() == govern.AgentKindExec {
			out = append(out, seat)
		}
	}
	for _, seat := range seats {
		if seat.Kind() != govern.AgentKindExec {
			out = append(out, seat)
		}
	}
	return out
}

// teamOrderFor 读该会话团队环的链表顺序（未装配团队环 → nil）。
func (g *goalCoordinator) teamOrderFor(sessionID string) []string {
	if g.deps.TeamRuntimeFor == nil {
		return nil
	}
	runtime := g.deps.TeamRuntimeFor(sessionID)
	if runtime == nil {
		return nil
	}
	return runtime.Order()
}

// teamRoleSeat 是按团队顺序装出来的座位（只包一个 Act，不引入第二套座位状态）。
type teamRoleSeat struct {
	name string
	kind govern.AgentKind
	act  func(context.Context) (govern.TurnAction, error)
}

func (s teamRoleSeat) Name() string           { return s.name }
func (s teamRoleSeat) Kind() govern.AgentKind { return s.kind }
func (s teamRoleSeat) Act(ctx context.Context) (govern.TurnAction, error) {
	if s.act == nil {
		return govern.TurnAction{}, nil
	}
	return s.act(ctx)
}

// Break 外部中断治理循环（无 Governor 时报错，对齐 headless 未装配语义）。
func (g *goalCoordinator) Break(_ context.Context, sessionID, reason string) error {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	if runtime == nil || runtime.gov == nil {
		return fmt.Errorf("goal_gov_break: 治理循环未装配（该会话尚无 Governor）")
	}
	runtime.gov.Break(reason)
	return nil
}

// setEvaluator 装配/替换 TL 评估器：更新后续会话 bundle 构造输入，并为已
// 存在的会话重建 Supervisor（重置 Governor，治理会话从下一轮重新 bind）。
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
		runtime.gov = nil
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
// LIFO 栈。每帧带自己的标题/陈述/状态/验收/最近进度，工作台据此按帧分块查看。
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
			UpdatedAt:  record.UpdatedAt,
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

// DirectivePublished 报告该 corr 的指令是否已回放进可见会话（corr 幂等去重）。
func (g *goalCoordinator) DirectivePublished(sessionID, corr string) bool {
	if corr == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.published[sessionID][corr]
}

// MarkDirectivePublished 记录一条指令已回放进可见会话（回合结束时记，下一次
// 回合的常规回放据此跳过它，避免同一裁决出现两行）。
func (g *goalCoordinator) MarkDirectivePublished(sessionID, corr string) {
	if corr == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.published[sessionID] == nil {
		g.published[sessionID] = make(map[string]bool)
	}
	g.published[sessionID][corr] = true
}

// NoteInjected 记录一次已注入引擎受信区的 TL 指令（回合尾可见区回放）。
func (g *goalCoordinator) NoteInjected(sessionID string, directives []goaldomain.TLDirective) {
	if len(directives) == 0 {
		return
	}
	g.mu.Lock()
	g.injections[sessionID] = append(g.injections[sessionID], directives...)
	g.mu.Unlock()
}

// TakeInjected 取走（并清空）该会话已注入受信区的 TL 指令。
func (g *goalCoordinator) TakeInjected(sessionID string) []goaldomain.TLDirective {
	g.mu.Lock()
	directives := g.injections[sessionID]
	delete(g.injections, sessionID)
	g.mu.Unlock()
	return directives
}

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
func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	roundError := g.roundError[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return nil
	}
	status := runtime.ctl.Status()
	if status.Active == nil {
		return &dto.GoalGovernanceView{
			Active: false, RoundLimit: goalLoopRoundLimit(g.deps.MaxRounds),
			RoundError: roundError,
		}
	}
	peer := runtime.sup.Snapshot()
	view := &dto.GoalGovernanceView{
		Active:     true,
		GoalID:     status.Active.ID,
		Title:      status.Active.Title,
		Status:     string(status.Active.Status),
		RoundLimit: goalLoopRoundLimit(g.deps.MaxRounds),
		PeerState:  string(peer.Peer),
		RoundError: roundError,
		// 进行中的 ADVISOR 正文（只读快照）：回合结束为空。前端据此在评审期间
		// 轮询快照，把"评审在写什么"及时渲染出来。
		InFlight:      peer.InFlight,
		InFlightChars: peer.InFlightChars,
		RoundSteps:    goalStepViews(peer.RoundSteps),
	}
	// 每帧的只读投影：工作台按**活动栈**分块展示（栈顶=当前目标，栈下=被嵌套
	// 压栈而暂停的目标）。栈只有一份事实（Controller 的 LIFO 栈），这里只读。
	view.Stack = goalStackFrames(status.Stack)
	if runtime.gov != nil {
		snapshot := govern.SnapshotOf(runtime.gov)
		view.Round = snapshot.Round
		view.CurrentSeat = snapshot.CurrentSeat
		view.Broken = snapshot.Broken
		view.BreakReason = snapshot.BreakReason
	}
	if rounds := peer.Rounds; len(rounds) > 0 {
		view.LastDirective = rounds[len(rounds)-1].Summary
	}
	return view
}
