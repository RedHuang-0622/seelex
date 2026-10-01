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
	"time"

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
	// SeatJobs 把一轮治理推进表达为作业（D4：座位循环不再是"与 jobs 并列的第二套
	// 驱动"）。nil = 宿主没装配座位作业面（测试桩宿主 / 未接线宿主）→ 走现状同步
	// 循环，行为一字不变。
	SeatJobs SeatJobs
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
	// 驱动唯一化（D4）：座位循环的正文只有一份（runSeatRound）。装配了座位作业面
	// 就把它表达为一个 jobs.KindSeat 作业（派发 → 有界汇合），没装配的宿主原地同步
	// 跑同一份正文——两条路径不复写循环。
	if jobs := g.deps.SeatJobs; jobs != nil {
		return g.advanceSeatViaJobs(ctx, jobs, sessionID, detail)
	}
	return g.runSeatRound(ctx, sessionID, detail)
}

// seatJoinBudget 是座位作业的**有界汇合预算**：座位循环在作业里跑，回合收尾不能
// 等到天荒地老；到点仍无终态按"本轮未完成"登记，并尽力终止该作业。
const seatJoinBudget = 5 * time.Minute

// seatJobStateDone 是座位作业"这一轮治理跑完了"的终态字面量（与 Seele
// jobs.StateDone 同值：端口用最小面字符串，不把框架类型拉进 goal 域）。
const seatJobStateDone = "done"

// advanceSeatViaJobs 把这一轮治理推进表达为一个座位作业：派发 → 有界汇合 → 按终态
// 折回失败原因（与 gov.Next 报错同一条登记路径，见 AdvanceAfterChat）。
//
// 它自己**不跑**座位：跑是执行体的事（Service.RunSeatRound，复用同一份
// runSeatRound）。归属与正文全走载荷——作业的执行 ctx 是 jobs.Manager 从
// Background 派生的，不带原调用的会话与 detail。
func (g *goalCoordinator) advanceSeatViaJobs(ctx context.Context, jobs SeatJobs, sessionID, detail string) error {
	handle, err := jobs.DispatchSeat(ctx, sessionID, detail)
	if err != nil {
		return err
	}
	outcome, err := jobs.JoinSeat(ctx, handle, seatJoinBudget)
	if err != nil {
		return err
	}
	if outcome.Known && outcome.State == seatJobStateDone {
		return nil
	}
	return errors.New(seatOutcomeError(outcome))
}

// seatOutcomeError 把非 done 的座位作业终态折成一句可读的失败原因（优先用作业面给
// 的有界摘要，没有就合成——绝不返回空串，否则 noteRoundError 会把失败当"无失败"）。
func seatOutcomeError(outcome SeatJobOutcome) string {
	if summary := strings.TrimSpace(outcome.Summary); summary != "" {
		return summary
	}
	if !outcome.Known {
		return "goal 座位作业终态未知（句柄已不在册）"
	}
	return fmt.Sprintf("goal 座位作业未完成：state=%s exit=%d", outcome.State, outcome.ExitCode)
}

// runSeatRound 是座位循环的**唯一正文**（驱动唯一化）：从当前轮次推进到 Round
// 递增或断环为止。同步降级路径（advanceAfterChat）与 jobs.KindSeat 执行体
// （Service.RunSeatRound）都调它——两份调用、一份实现。
func (g *goalCoordinator) runSeatRound(ctx context.Context, sessionID, detail string) error {
	runtime := g.bundleFor(sessionID)
	if runtime.gov == nil {
		runtime.gov = g.newGovernor(sessionID, runtime)
	}
	// Governor.Next 每次只推进一个座位；推进一整轮（让位 → 评审）需要执行到
	// Round 递增或断环为止。
	//
	// 上限按**实际座位数**取，不能写死：座位数由团队链派生（EXEC + ADVISOR 是
	// 下限，链上还有别的 kind 时更多），写死 2 会让多座链永远走不完一轮
	// （Round 永不递增 → 轮次上限形同虚设、每轮只推进前两个座位）。
	//
	// detail 是这一轮的"工作正文"。它**不再进治理环**：环里只剩让位座（main/EXEC）
	// 与评审座（techlead/ADVISOR），两者都不消费正文——员工干活由 leader 派 worker
	// 作业（team_dispatch），正文随作业载荷走。它在派发侧仍有用途（座位作业的行标题
	// 就来自它：seatJobDescription），本函数因此把它显式忽略，而不是悄悄丢掉。
	_ = detail
	roundBefore := runtime.gov.Round()
	attempts := len(runtime.gov.Seats()) + 1
	if attempts < 3 {
		attempts = 3
	}
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
// 座位只回答"谁在链上、是什么 kind"：员工不再由治理环驱动（leader 用 team_dispatch
// 派 worker 作业，见 docs/arch/teamwork-leader-worker-architecture.md），治理环只为
// main（EXEC 让位）与 techlead（ADVISOR 评审）派生座位。
type RoleSeat struct {
	RoleName string
	RoleKind dto.RoleKind
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
				Seats:      specs,
				Supervisor: supervisor,
				ExecAct:    execAct,
			}
			if seats := plan.seats(); len(seats) > 0 {
				return seats
			}
		}
	}
	return seatsFromOrder(g.teamOrderFor(sessionID), supervisor, execAct)
}

// seatPlan 是座位派生的一份输入快照（纯数据：单测可直接构造它钉住映射）。
type seatPlan struct {
	Seats      []RoleSeat
	Supervisor *goaldomain.Supervisor
	ExecAct    func(context.Context) (govern.TurnAction, error)
}

// seats 按角色 kind 派生座位（纯函数，便于单测钉住"改名不丢座位"）。
//
// 映射：
//   - main      → EXEC 座位：用户/主代理的会谈本身就是执行面（外部驱动），
//     座位只让位、不代跑，避免同一份工作在两条路上各跑一次
//   - techlead  → ADVISOR 座位（评审者）
//   - 其余 kind → 无治理座位：员工干活走 leader 派发的 worker 作业（team_dispatch），
//     不再由治理环按"席位"驱动（2026-10-01 席位制轮回的退场点）
func (plan seatPlan) seats() []govern.Seat {
	seats := make([]govern.Seat, 0, len(plan.Seats))
	for _, seat := range plan.Seats {
		switch seat.RoleKind {
		case dto.RoleKindMain:
			seats = append(seats, teamRoleSeat{name: "exec-a", kind: govern.AgentKindExec, act: plan.ExecAct})
		case dto.RoleKindTechlead:
			seats = append(seats, goaldomain.NewAdvisorSeat(plan.Supervisor, ""))
		}
	}
	return seats
}

// SeatJobOutcome 是座位作业的终态读数。它**定义在 contract/dto**（这里是别名）：
// goal 域声明端口、seelebridge 实现端口，两端都要引用同一个类型，而 seelebridge
// 不能反向 import application/core（core 已 import seelebridge，会成环）。
type SeatJobOutcome = dto.SeatJobOutcome

// SeatJobs 把一轮治理推进表达为作业（D4：座位循环不再是"与 jobs 并列的第二套
// 驱动"）。由 seelebridge 的 Runtime 实现（DispatchSeat → 作业面派发，
// JoinSeat → 有界汇合到终态），经 goalCoordinatorDeps.SeatJobs 注入。
//
// 未实现（测试桩宿主 / 未接线宿主）= nil → advanceAfterChat 走现状同步循环：端口
// 缺席不该改变任何既有行为。
type SeatJobs interface {
	DispatchSeat(ctx context.Context, sessionID, detail string) (handle string, err error)
	JoinSeat(ctx context.Context, handle string, budget time.Duration) (SeatJobOutcome, error)
}

// SeatJobsAssembled 是座位作业面的**装配探针**（可选能力，与 SessionContextStoreFor
// 同一口径）：实现了 SeatJobs 还不够——作业面（jobs.Manager）要到装配期注入
// teamwork 后端才存在，没装配的宿主必须保持"端口为 nil"的现状语义（同步座位循环，
// 行为一字不变），而不是接到一个永远报错的空壳。
type SeatJobsAssembled interface {
	SeatJobsAssembled() bool
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
