package core

// goal_coordinator.go — 会话级 goal 协调器。
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
	"github.com/RedHuang-0622/seelex/store"
)

// frameProgressLimit 是看板卡片上"一行摘要"保留的打点条数（详情面看 ProgressAll
// 全量）：看板帧投影与"存档恢复"的帧投影共用同一个上限。
const frameProgressLimit = 3

// goalCoordinatorDeps 是 goal 协调器装配输入。
type goalCoordinatorDeps struct {
	// StoreFor 返回指定会话的 SessionContextStore（nil = 未装配 → 内存态）。
	StoreFor func(sessionID string) *store.SessionContextStore
}

// goalSessionRuntime 是一个会话的 goal bundle（会话间零共享）。
type goalSessionRuntime struct {
	ctl *goaldomain.Controller
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

// ForgetSession 释放一个会话的 goal 运行时（会话释放面调用）：丢掉按会话缓存
// 的控制器与上下文存储适配器，下次经 StoreFor 重新解析、按盘重建。
func (g *goalCoordinator) ForgetSession(sessionID string) {
	if g == nil || sessionID == "" {
		return
	}
	g.mu.Lock()
	delete(g.sessions, sessionID)
	g.mu.Unlock()
}

// bundleFor 返回（需要时创建）指定会话的 goal bundle。
func (g *goalCoordinator) bundleFor(sessionID string) *goalSessionRuntime {
	g.mu.Lock()
	defer g.mu.Unlock()
	if runtime := g.sessions[sessionID]; runtime != nil {
		return runtime
	}
	var st goaldomain.Store
	var audit goaldomain.AuditAccount
	if g.deps.StoreFor != nil {
		if contextStore := g.deps.StoreFor(sessionID); contextStore != nil {
			adapter := goaldomain.NewContextStateStore(contextStore)
			if boards, ok := g.boardFor(sessionID); ok {
				adapter.BindBoards(boards)
			}
			st, audit = adapter, adapter
		}
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
	runtime := &goalSessionRuntime{ctl: controller}
	g.sessions[sessionID] = runtime
	return runtime
}

// Begin 注册并压栈（会话路由）。
func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return g.bundleFor(sessionID).ctl.Begin(ctx, request)
}

// Update 更新栈顶（会话路由）。
func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return g.bundleFor(sessionID).ctl.Update(ctx, request)
}

// FinishDirect 直接落终态收口（goal_done 的领域动作）：把栈顶 goal 标
// completed / aborted 并弹栈。
func (g *goalCoordinator) FinishDirect(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	controller := g.bundleFor(sessionID).ctl
	if abort {
		return controller.Abort(ctx, request)
	}
	return controller.Finish(ctx, request)
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

// goalStackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
// = active 那一帧）。
func goalStackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView {
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

// GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal/无存档 → nil）。
//
// 看板存档（§8/§9 的读侧兜底）：活体栈为空时存档是唯一还在的快照——进程重启后
// 活栈通道可能还没重建（或已经被弹空），而看板存档里 state=active 说明那一刻有
// 目标在跑。此时按存档的 active 帧把看板重建出来并标 Recovered=true。
func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	g.mu.Lock()
	runtime := g.sessions[sessionID]
	g.mu.Unlock()
	archive, hasArchive := g.readGoalBoardArchive(sessionID)
	if runtime == nil {
		return recoveredGoalView(archive, hasArchive)
	}
	status := runtime.ctl.Status()
	if status.Active == nil {
		if view := recoveredGoalView(archive, hasArchive); view != nil {
			return view
		}
		return &dto.GoalGovernanceView{Active: false, History: goalHistoryViews(archive, hasArchive)}
	}
	return &dto.GoalGovernanceView{
		Active:  true,
		GoalID:  status.Active.ID,
		Title:   status.Active.Title,
		Status:  status.Active.Status,
		History: goalHistoryViews(archive, hasArchive),
		Stack:   goalStackFrames(status.Stack),
	}
}

// boardFor 返回指定会话的看板存档取用面（未装配 → false）。
func (g *goalCoordinator) boardFor(sessionID string) (store.SessionBoards, bool) {
	if g == nil || g.deps.StoreFor == nil {
		return nil, false
	}
	contextStore := g.deps.StoreFor(sessionID)
	if contextStore == nil {
		return nil, false
	}
	router := contextStore.Router()
	if router == nil {
		return nil, false
	}
	return router.BoardsForSession(sessionID)
}

// readGoalBoardArchive 读取该会话的看板存档（缺失/损坏 → false）。
func (g *goalCoordinator) readGoalBoardArchive(sessionID string) (store.GoalBoardMeta, bool) {
	boards, ok := g.boardFor(sessionID)
	if !ok {
		return store.GoalBoardMeta{}, false
	}
	archive, err := boards.ReadGoalBoard(context.Background())
	if err != nil {
		return store.GoalBoardMeta{}, false
	}
	return archive, true
}

// recoveredGoalView 用存档里的 active 帧重建看板（Recovered=true）。
func recoveredGoalView(archive store.GoalBoardMeta, hasArchive bool) *dto.GoalGovernanceView {
	if !hasArchive || archive.State != store.BoardStateActive || archive.Active == nil {
		return nil
	}
	active := archive.Active
	progress := goalProgressViews(active.Progress)
	return &dto.GoalGovernanceView{
		Active:    true,
		Recovered: true,
		GoalID:    active.GoalID,
		Title:     active.Title,
		Status:    goaldomain.StatusOfRecord(active.Status),
		History:   goalHistoryViews(archive, hasArchive),
		Stack: []dto.GoalFrameView{{
			ID:          active.GoalID,
			Title:       active.Title,
			Statement:   active.Statement,
			Status:      active.Status,
			Active:      true,
			Acceptance:  append([]string(nil), active.Acceptance...),
			OutOfScope:  append([]string(nil), active.OutOfScope...),
			Progress:    tailProgressViews(progress, frameProgressLimit),
			ProgressAll: progress,
			CreatedAt:   active.CreatedAt,
			UpdatedAt:   active.UpdatedAt,
		}},
	}
}

// goalHistoryViews 把看板存档的收口账本投影成只读 DTO（空 → nil）。
func goalHistoryViews(archive store.GoalBoardMeta, hasArchive bool) []dto.GoalHistoryView {
	if !hasArchive || len(archive.History) == 0 {
		return nil
	}
	views := make([]dto.GoalHistoryView, 0, len(archive.History))
	for _, entry := range archive.History {
		views = append(views, dto.GoalHistoryView{
			GoalID:        entry.GoalID,
			Title:         entry.Title,
			Status:        entry.Status,
			ClosedAt:      entry.ClosedAt,
			ClosedReason:  entry.ClosedReason,
			ProgressCount: entry.ProgressCount,
		})
	}
	return views
}

// goalProgressViews 把看板存档里的打点投影成只读 DTO（空 → nil）。
func goalProgressViews(items []store.GoalProgress) []dto.GoalProgressView {
	if len(items) == 0 {
		return nil
	}
	views := make([]dto.GoalProgressView, 0, len(items))
	for _, item := range items {
		views = append(views, dto.GoalProgressView{At: item.At, Kind: item.Kind, Content: item.Content})
	}
	return views
}

// tailProgressViews 取末尾 limit 条（卡片一行摘要用；详情面用全量）。
func tailProgressViews(items []dto.GoalProgressView, limit int) []dto.GoalProgressView {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}
