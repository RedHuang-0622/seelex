package goal

// coordinator.go — 会话级 goal 协调器。
//
// 职责：按 sessionID 持有 goal.Controller，提供 Service 侧的 goal 方法面与
// 只读治理视图（GoalGovernanceView）。goal 状态/审计落 store 第五栈
// （ContextStateStore），未装配会话上下文存储时退化为内存（Store=nil）。
//
// **TL/ADVISOR 评审环已删除（2026-10-17 用户裁决）**：目标由主代理自己推进、
// `goal_done` 直接收口；不再有 b 回合、终态 gate、TL 指令队列或评审过程投影。
// 团队编排由 teamwork 承担。

import (
	"context"
	"sync"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// CoordinatorDeps is the session goal coordinator assembly input.
type CoordinatorDeps struct {
	// StoreFor returns the goal domain store and audit sink for one session.
	// Both nil means the controller runs in memory.
	StoreFor func(sessionID string) (goaldomain.Store, goaldomain.AuditAccount)
	// ArchiveFor projects the persisted goal board for one session. The bool
	// reports whether an archive exists; conversion from Store shapes belongs
	// to Wiring, not this application package.
	ArchiveFor func(context.Context, string) (dto.GoalGovernanceView, bool)
}

// goalSessionRuntime 是一个会话的 goal bundle（会话间零共享）。
type sessionRuntime struct {
	ctl *goaldomain.Controller
}

// Coordinator owns the per-session goal controller cache and governance
// projection. Goal state remains in backend/multiagent/goal.
type Coordinator struct {
	mu       sync.Mutex
	deps     CoordinatorDeps
	sessions map[string]*sessionRuntime
}

// NewCoordinator constructs the session goal coordinator.
func NewCoordinator(deps CoordinatorDeps) *Coordinator {
	return &Coordinator{
		deps:     deps,
		sessions: make(map[string]*sessionRuntime),
	}
}

// ForgetSession 释放一个会话的 goal 运行时（会话释放面调用）：丢掉按会话缓存
// 的控制器与上下文存储适配器，下次经 StoreFor 重新解析、按盘重建。
func (g *Coordinator) ForgetSession(sessionID string) {
	if g == nil || sessionID == "" {
		return
	}
	g.mu.Lock()
	delete(g.sessions, sessionID)
	g.mu.Unlock()
}

// bundleFor 返回（需要时创建）指定会话的 goal bundle。
func (g *Coordinator) bundleFor(sessionID string) *sessionRuntime {
	g.mu.Lock()
	defer g.mu.Unlock()
	if runtime := g.sessions[sessionID]; runtime != nil {
		return runtime
	}
	var st goaldomain.Store
	var audit goaldomain.AuditAccount
	if g.deps.StoreFor != nil {
		st, audit = g.deps.StoreFor(sessionID)
	}
	controller := goaldomain.NewController(goaldomain.Options{
		Depth: goaldomain.DefaultStackDepth, Store: st, Audit: audit,
	})
	if st != nil {
		if err := controller.Reload(context.Background()); err != nil {
			controller = goaldomain.NewController(goaldomain.Options{
				Depth: goaldomain.DefaultStackDepth,
			})
		}
	}
	runtime := &sessionRuntime{ctl: controller}
	g.sessions[sessionID] = runtime
	return runtime
}

// Begin 注册并压栈（会话路由）。
func (g *Coordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return g.bundleFor(sessionID).ctl.Begin(ctx, request)
}

// Update 更新栈顶（会话路由）。
func (g *Coordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return g.bundleFor(sessionID).ctl.Update(ctx, request)
}

// FinishDirect 直接落终态收口（goal_done 的领域动作）：把栈顶 goal 标
// completed / aborted 并弹栈。
func (g *Coordinator) FinishDirect(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	controller := g.bundleFor(sessionID).ctl
	if abort {
		return controller.Abort(ctx, request)
	}
	return controller.Finish(ctx, request)
}

// StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
func (g *Coordinator) StatusFor(sessionID string) goaldomain.StatusView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	if runtime == nil {
		return goaldomain.StatusView{}
	}
	return runtime.ctl.Status()
}

// StackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
// = active 那一帧）。
func StackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView {
	if len(stack) == 0 {
		return nil
	}
	frames := make([]dto.GoalFrameView, 0, len(stack))
	for index, record := range stack {
		if record == nil {
			continue
		}
		frame := dto.GoalFrameView{
			ID:         record.ID,
			Title:      record.Title,
			Statement:  record.Statement,
			Status:     record.Status.String(),
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
		if len(progress) > dto.GoalFrameProgressPreviewLimit {
			progress = progress[len(progress)-dto.GoalFrameProgressPreviewLimit:]
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

// GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal/无存档 → nil）。
//
// 看板存档（§8/§9 的读侧兜底）：活体栈为空时存档是唯一还在的快照——进程重启后
// 活栈通道可能还没重建（或已经被弹空），而看板存档里 state=active 说明那一刻有
// 目标在跑。此时按存档的 active 帧把看板重建出来并标 Recovered=true。
func (g *Coordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	archive, hasArchive := g.readGoalBoardArchive(sessionID)
	if runtime == nil {
		if !hasArchive {
			return nil
		}
		return &archive
	}
	status := runtime.ctl.Status()
	if status.Active == nil {
		if !hasArchive {
			return &dto.GoalGovernanceView{Active: false}
		}
		if archive.Active {
			return &archive
		}
		return &dto.GoalGovernanceView{Active: false, History: archive.History}
	}
	return &dto.GoalGovernanceView{
		Active:  true,
		GoalID:  status.Active.ID,
		Title:   status.Active.Title,
		Status:  status.Active.Status,
		History: archive.History,
		Stack:   StackFrames(status.Stack),
	}
}

// readGoalBoardArchive 读取该会话的看板存档（缺失/损坏 → false）。
func (g *Coordinator) readGoalBoardArchive(sessionID string) (dto.GoalGovernanceView, bool) {
	if g == nil || g.deps.ArchiveFor == nil {
		return dto.GoalGovernanceView{}, false
	}
	return g.deps.ArchiveFor(context.Background(), sessionID)
}
