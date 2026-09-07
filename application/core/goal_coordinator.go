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
	"fmt"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
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
	// MaxRounds 是治理循环轮次护栏（≤0 = 不设上限，由裁决/Break 收束）。
	MaxRounds int
}

// goalSessionRuntime 是一个会话的 goal 治理 bundle（会话间零共享）。
type goalSessionRuntime struct {
	ctl *goaldomain.Controller
	sup *goaldomain.Supervisor
	gov govern.Governor // 惰性装配（首次 Next）
}

type goalCoordinator struct {
	mu       sync.Mutex
	now      func() int64
	deps     goalCoordinatorDeps
	sessions map[string]*goalSessionRuntime

	heartbeatSeq map[string]uint64
	heartbeatAt  map[string]int64
	injections   map[string][]string // TL 指令已注入引擎的可见副本（回合尾展示）
}

func newGoalCoordinator(deps goalCoordinatorDeps) *goalCoordinator {
	return &goalCoordinator{
		now:          func() int64 { return time.Now().Unix() },
		deps:         deps,
		sessions:     make(map[string]*goalSessionRuntime),
		heartbeatSeq: make(map[string]uint64),
		heartbeatAt:  make(map[string]int64),
		injections:   make(map[string][]string),
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
	g.sessions[sessionID] = runtime
	return runtime
}

func (g *goalCoordinator) bumpHeartbeat(sessionID string) {
	g.mu.Lock()
	g.heartbeatSeq[sessionID]++
	g.heartbeatAt[sessionID] = g.now()
	g.mu.Unlock()
}

// Begin 注册并压栈（会话路由）。
func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	record, err := g.bundleFor(sessionID).ctl.Begin(ctx, request)
	if err == nil {
		g.bumpHeartbeat(sessionID)
	}
	return record, err
}

// Update 更新栈顶（会话路由）。
func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	record, err := g.bundleFor(sessionID).ctl.Update(ctx, request)
	if err == nil {
		g.bumpHeartbeat(sessionID)
	}
	return record, err
}

// ProposeFinish 送终态 gate（TL 缺席时 OutcomeNoTL 直连收口；B4）。
func (g *goalCoordinator) ProposeFinish(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error) {
	result, err := g.bundleFor(sessionID).sup.ProposeFinish(ctx, request)
	if err == nil {
		g.bumpHeartbeat(sessionID)
	}
	return result, err
}

// Notify 登记 a 事件（exec 账本；触发策略见 Supervisor）。
func (g *goalCoordinator) Notify(ctx context.Context, sessionID string, signal goaldomain.TLEvalSignal) error {
	err := g.bundleFor(sessionID).sup.Notify(ctx, signal)
	if err == nil {
		g.bumpHeartbeat(sessionID)
	}
	return err
}

// Next 推进治理循环一轮（惰性装配 EXEC+ADVISOR 双座位；返回 false = 收束）。
func (g *goalCoordinator) Next(ctx context.Context, sessionID string) (bool, error) {
	runtime := g.bundleFor(sessionID)
	if runtime.gov == nil {
		execAct := func(context.Context) (govern.TurnAction, error) {
			return govern.TurnAction{}, nil // EXEC 侧外部驱动，座位仅让位
		}
		runtime.gov = goaldomain.NewTurnGovernorForDSA2A("exec-a", execAct, runtime.sup, g.deps.MaxRounds)
	}
	more, err := runtime.gov.Next(ctx)
	if err == nil {
		g.bumpHeartbeat(sessionID)
	}
	return more, err
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
	g.bumpHeartbeat(sessionID)
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
	for _, runtime := range g.sessions {
		runtime.sup = goaldomain.NewSupervisor(runtime.ctl, evaluator, goaldomain.DefaultTechLeaderConfig())
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

// NoteInjected 记录一次已注入引擎的 TL 指令文本（回合尾可见区回放）。
func (g *goalCoordinator) NoteInjected(sessionID string, texts []string) {
	if len(texts) == 0 {
		return
	}
	g.mu.Lock()
	g.injections[sessionID] = append(g.injections[sessionID], texts...)
	g.mu.Unlock()
}

// TakeInjected 取走（并清空）该会话已注入的 TL 指令文本。
func (g *goalCoordinator) TakeInjected(sessionID string) []string {
	g.mu.Lock()
	texts := g.injections[sessionID]
	delete(g.injections, sessionID)
	g.mu.Unlock()
	return texts
}

// GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal → nil，前端隐藏）。
func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	seq, at := g.heartbeatSeq[sessionID], g.heartbeatAt[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return nil
	}
	status := runtime.ctl.Status()
	if status.Active == nil {
		return &dto.GoalGovernanceView{Active: false, HeartbeatAt: at, HeartbeatSeq: seq}
	}
	peer := runtime.sup.Snapshot()
	view := &dto.GoalGovernanceView{
		Active:       true,
		GoalID:       status.Active.ID,
		Title:        status.Active.Title,
		Status:       string(status.Active.Status),
		PeerState:    string(peer.Peer),
		HeartbeatAt:  at,
		HeartbeatSeq: seq,
	}
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
