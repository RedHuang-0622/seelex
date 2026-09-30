package seelebridge

// runtime_teamwork.go — teamwork 编排面在 seelebridge 的落点：leader 的硬编排工具
// （team_plan / team_dispatch / team_join / team_milestone / team_retire）+ 框架通用
// 管理工具 jobs_manage，以及它们背后的作业面与 Coordinator 组装。
//
// 分工（docs/arch/teamwork-leader-worker-architecture.md §2 / §4.5 / D1）：
//   - **作业面**归 Seele 的 jobs 根能力（契约 + Manager + jobs_manage）；
//   - **硬编排**归 seelebridge/teamwork（计划 + 派发 + 汇合 + 里程碑 + 退场）；
//   - **执行体 / 工作区 / 会话复位**是端口，由本文件把 Runtime 的能力接上去：
//     worker 执行体 = 角色会话里跑一轮有界回合；worktree = 释放 git worktree；
//     session reset = 清角色会话的工作历史（保留在编）。
//
// 装配口径：backend（PlanStore + KeyFor）由**组合根**注入。未注入 = 不注册这族
// 工具——"有就有、没有就是没装配"，而不是注册一堆永远报错的空壳。
//
// 铁律（§6.1）：框架绝不把作业结果 push 进忙会话。作业完成经 jobs.Events() 的
// 变更信号口与回合边界有界摘要浮现；本文件不引入任何唤醒路径。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/Seele/jobs/builtin"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TeamworkBackend 是组合根注入的 teamwork 持久面与作用域解析。
type TeamworkBackend struct {
	// Store 是计划与审计的持久面（生产实现 = sessionstore 的 moduleTeamwork）。
	Store teamwork.PlanStore
	// KeyFor 把会话 ID 解析成存储作用域键（project_id + session_id）。
	KeyFor func(sessionID string) (sessionstore.Key, bool)
	// MaxTeammates 是产品级人数上限（seelexctx.TeamLimits.MaxTeammates）。
	MaxTeammates int
	// MaxTurns 是每个 teammate 作业的回合上限（0 = 装配层默认）。
	MaxTurns int
}

// SetTeamworkBackend 注入 teamwork 持久面（组合根在装配期调用一次）。
//
// 它同时把 jobs.Manager 装配起来（注册 worker 执行体与服务解析器），因此作业面
// 只在真有 teammate 编排需求时存在；未注入 backend 的宿主（测试/桩）不会多出一个
// 进程内作业管理器。
func (r *Runtime) SetTeamworkBackend(backend TeamworkBackend) error {
	if r == nil {
		return errors.New("teamwork: runtime 为空")
	}
	if backend.Store == nil || backend.KeyFor == nil {
		return errors.New("teamwork: SetTeamworkBackend 需要 PlanStore 与 KeyFor")
	}
	manager, err := jobs.New(
		jobs.WithExecutor(teamwork.WorkerExecutor(r, backend.MaxTurns)),
		// 座位执行体（D4）：goal 座位循环是 jobs 契约下的一个实现，不再是"与 jobs
		// 并列的第二套驱动"。它拿着 Runtime 自身——真跑一轮由 SetSeatRoundRunner
		// 注入的 goal 域实现负责（未注入 = 执行体显式报错，不静默降级成空成功）。
		jobs.WithExecutor(teamwork.SeatExecutor(r)),
		jobs.WithSessionResolver(func(ctx context.Context) string {
			return seeletelemetry.SessionIDFromContext(ctx)
		}),
	)
	if err != nil {
		return fmt.Errorf("teamwork: 装配 jobs.Manager 失败: %w", err)
	}
	r.teamworkMu.Lock()
	r.teamworkBackend = &backend
	r.teamworkJobs = manager
	r.teamworkCoords = map[sessionstore.Key]*teamwork.Coordinator{}
	r.lifecycle = append(r.lifecycle, func() { _ = manager.Close(context.Background()) })
	r.teamworkMu.Unlock()
	// 装配即注册 leader 工具面：RegisterBuiltins 在组合根更早处跑（那时 router /
	// workspace 还没就绪），因此工具注册跟在 backend 注入之后。
	r.registerTeamworkTools()
	return nil
}

// teamworkEnabled 报告 leader 编排是否已装配（工具注册的前置条件）。
func (r *Runtime) teamworkEnabled() bool {
	if r == nil {
		return false
	}
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	return r.teamworkBackend != nil && r.teamworkJobs != nil
}

// coordinatorFor 取（必要时建）当前会话的 Coordinator。
//
// 缓存按**会话作用域键**（project_id + session_id）：同一会话的多次工具调用共享
// 一个 Coordinator（共享作业表与计划），跨会话不串。作业句柄只在内存（jobs I-4），
// 因此进程重启后缓存自然作废。
func (r *Runtime) coordinatorFor(ctx context.Context) (*teamwork.Coordinator, error) {
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	backend, manager := r.teamworkBackend, r.teamworkJobs
	if backend == nil || manager == nil {
		return nil, errors.New("teamwork: leader 编排未装配（缺 SetTeamworkBackend）")
	}
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, errors.New("teamwork: 当前调用没有会话归属（team_* 工具必须在会话回合内调用）")
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok || strings.TrimSpace(key.ProjectID) == "" || strings.TrimSpace(key.SessionID) == "" {
		return nil, fmt.Errorf("teamwork: 无法解析会话 %q 的作用域键（项目/会话）", sessionID)
	}
	if existing := r.teamworkCoords[key]; existing != nil {
		return existing, nil
	}
	coordinator, err := teamwork.New(teamwork.Options{
		Key:          key,
		Store:        backend.Store,
		Jobs:         manager,
		Workers:      r,
		Worktrees:    r,
		Sessions:     r,
		MaxTeammates: backend.MaxTeammates,
		MaxTurns:     backend.MaxTurns,
	})
	if err != nil {
		return nil, err
	}
	if r.teamworkCoords == nil {
		r.teamworkCoords = map[sessionstore.Key]*teamwork.Coordinator{}
	}
	r.teamworkCoords[key] = coordinator
	return coordinator, nil
}

// registerTeamworkTools 注册 leader 六件套（RegisterBuiltins 内调用；未装配 backend
// 时注册面为空——没有后端就不摆出一族永远报错的工具）。
func (r *Runtime) registerTeamworkTools() {
	if !r.teamworkEnabled() {
		return
	}
	r.RegisterTool("team_plan", teamworkPlanDescription(), teamworkPlanSchema(), r.teamPlanHandler)
	r.RegisterTool("team_dispatch", teamworkDispatchDescription(), teamworkDispatchSchema(), r.teamDispatchHandler)
	r.RegisterTool("team_join", teamworkJoinDescription(), teamworkJoinSchema(), r.teamJoinHandler)
	r.RegisterTool("team_milestone", teamworkMilestoneDescription(), teamworkMilestoneSchema(), r.teamMilestoneHandler)
	r.RegisterTool("team_retire", teamworkRetireDescription(), teamworkRetireSchema(), r.teamRetireHandler)
	// jobs_manage：框架通用管理工具（jobs/builtin）。簇属按 seelex 的路由组表声明，
	// 与 bash_bg/job_manage 同组（它就是对作业面的读/写/销项）。
	r.teamworkMu.Lock()
	manager := r.teamworkJobs
	r.teamworkMu.Unlock()
	if manager != nil && r.registry != nil && r.registry.Registry != nil {
		provider := builtin.New(manager, builtin.WithMeta(seeltools.DeclaredToolMeta("jobs_manage")))
		_ = r.registry.Registry.Register(provider)
	}
}

// ── leader 工具：输入解析 + 转调 Coordinator ─────────────────────────────

func (r *Runtime) teamPlanHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		TeamID     string                      `json:"team_id"`
		Version    int                         `json:"version"`
		Stages     []sessionstore.TeamworkStage     `json:"stages"`
		Members    []sessionstore.TeamworkMember    `json:"members"`
		Milestones []sessionstore.TeamworkMilestone `json:"milestones"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_plan: 参数解析失败: %w", err)
	}
	if raw.Version == 0 {
		raw.Version = 1
	}
	plan := sessionstore.TeamworkPlan{
		TeamID: raw.TeamID, Version: raw.Version,
		Stages: raw.Stages, Members: raw.Members, Milestones: raw.Milestones,
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	if err := coordinator.SetPlan(ctx, plan); err != nil {
		return "", fmt.Errorf("team_plan: %w", err)
	}
	return jsonReceipt(map[string]any{
		"ok": true, "team_id": plan.TeamID, "stages": len(plan.Stages),
		"members": len(plan.Members), "milestones": len(plan.Milestones),
	})
}

func (r *Runtime) teamDispatchHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Role string `json:"role"`
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_dispatch: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	handle, stage, err := coordinator.Dispatch(ctx, strings.TrimSpace(raw.Role), raw.Goal)
	if err != nil {
		return "", fmt.Errorf("team_dispatch: %w", err)
	}
	return jsonReceipt(map[string]any{
		"ok": true, "handle": string(handle), "stage": stage,
		"hint": "受理回执即返回，不等待：继续你的关键路径，需要时用 jobs_manage(op=observe/fetch) 或 team_join 观察。",
	})
}

func (r *Runtime) teamJoinHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Handles  []string `json:"handles"`
		BudgetMS int      `json:"budget_ms"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_join: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	handles := make([]jobs.Handle, 0, len(raw.Handles))
	for _, handle := range raw.Handles {
		if trimmed := strings.TrimSpace(handle); trimmed != "" {
			handles = append(handles, jobs.Handle(trimmed))
		}
	}
	budget := time.Duration(raw.BudgetMS) * time.Millisecond
	if budget <= 0 {
		budget = 5 * time.Second
	}
	records, err := coordinator.Join(ctx, handles, budget)
	if err != nil {
		return "", fmt.Errorf("team_join: %w", err)
	}
	return jsonReceipt(map[string]any{"ok": true, "jobs": records})
}

func (r *Runtime) teamMilestoneHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_milestone: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	if err := coordinator.Milestone(ctx, strings.TrimSpace(raw.ID), raw.Content); err != nil {
		return "", fmt.Errorf("team_milestone: %w", err)
	}
	return jsonReceipt(map[string]any{"ok": true, "id": raw.ID})
}

func (r *Runtime) teamRetireHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_retire: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	if err := coordinator.Retire(ctx, strings.TrimSpace(raw.Role)); err != nil {
		return "", fmt.Errorf("team_retire: %w", err)
	}
	return jsonReceipt(map[string]any{
		"ok": true, "role": raw.Role,
		"detail": "回收作业 → 释放 worktree → 清会话内容 → 保在线",
	})
}

// RunWorker 实现 teamwork.WorkerRunner：在角色会话里跑一轮有界回合。
//
// 执行体只跑、不判定顺序、不回收工作区——那些是 Coordinator 的事（leader 掌控
// 顺序，执行体只管跑）。
func (r *Runtime) RunWorker(ctx context.Context, request teamwork.WorkerRequest, sink jobs.Sink) error {
	if r == nil {
		return errors.New("teamwork: worker 执行体未装配（runtime 为空）")
	}
	mainSessionID := strings.TrimSpace(request.MainSessionID)
	if mainSessionID == "" {
		mainSessionID = seeletelemetry.SessionIDFromContext(ctx)
	}
	sink.Progress(fmt.Sprintf("worker 起跑 role=%s stage=%s worktree=%s", request.Role, request.Stage, request.Worktree))
	r.bindWorkerProjectRoot(mainSessionID, request.RoleSessionID, request.Worktree)
	maxLoops := request.MaxTurns
	if maxLoops <= 0 {
		maxLoops = roleTurnMaxLoops
	}
	output, err := r.runRoleRound(ctx, roleRoundSpec{
		MainSessionID:    mainSessionID,
		RoleName:         request.Role,
		RoleSessionID:    request.RoleSessionID,
		ToolsPolicy:      request.ToolsPolicy,
		PermissionGroups: request.PermissionGroups,
		SystemPrompt:     r.roleTurnSystemPrompt(request.Role),
		Input:            workerRoundInput(request),
		MaxLoops:         maxLoops,
	})
	if err != nil {
		return err
	}
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		sink.Note(trimmed + "\n")
	}
	return nil
}

// bindWorkerProjectRoot 把角色会话的工具根绑到它这一轮该看到的工作区：有 worktree
// 指派且已建现场就绑 worktree，否则回退主会话项目根（与 worktree_manager.Begin 的
// 降级语义一致——缺失不等于失败）。
func (r *Runtime) bindWorkerProjectRoot(mainSessionID, roleSessionID, worktreeName string) {
	if r == nil || r.projectScope == nil || strings.TrimSpace(roleSessionID) == "" {
		return
	}
	root := ""
	if name := strings.TrimSpace(worktreeName); name != "" && r.worktreeMgr != nil {
		if info, ok := r.worktreeMgr.Info(name); ok {
			root = strings.TrimSpace(info.Path)
		}
	}
	if root == "" {
		root = r.projectScope.RootFor(mainSessionID)
	}
	if root == "" {
		return
	}
	_ = r.projectScope.BindFor(roleSessionID, root)
}

// ReleaseWorkspace 实现 teamwork.WorkspaceReleaser：释放一个 teammate 的工作区。
//
// 脏工作区**显式报错**（ErrUncommittedChanges 语义）而不是静默丢弃——现场是人的
// 资产，框架不替人做"丢还是留"的决定。无现场 = 无可释放（幂等）。
func (r *Runtime) ReleaseWorkspace(ctx context.Context, role string) error {
	if r == nil || r.worktreeMgr == nil {
		return errors.New("teamwork: 释放工作区需要 worktree 管理器（未装配）")
	}
	info, ok := r.worktreeMgr.Info(role)
	if !ok {
		return nil
	}
	if strings.TrimSpace(info.Path) != "" {
		if dirty, err := worktreeDirty(info.Path); err != nil {
			return fmt.Errorf("teamwork: 释放工作区前检查失败: %w", err)
		} else if dirty {
			return fmt.Errorf("teamwork: teammate %q 的工作区有未提交改动（%s）: %w", role, info.Path, worktree.ErrUncommittedChanges)
		}
	}
	mainSessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	if r.projectScope != nil && mainSessionID != "" {
		if root := strings.TrimSpace(r.projectScope.RootFor(mainSessionID)); root != "" {
			_ = worktree.CleanupWorktree(root, &worktree.NodeWorktree{Path: info.Path, Branch: info.Branch})
		}
	}
	r.worktreeMgr.Release(role)
	return nil
}

// ResetSession 实现 teamwork.SessionResetter：清一个角色会话的**记录内容**
// （工作历史），保留在编。角色会话是**进程内**执行面（刻意不接 DurableHistory），
// 因此清内存历史 + 落掉引擎槽即"内容已清"；下一次派发以干净上下文重开。
func (r *Runtime) ResetSession(_ context.Context, roleSessionID string) error {
	if r == nil || strings.TrimSpace(roleSessionID) == "" {
		return nil
	}
	state := r.roleTurnState()
	state.mu.Lock()
	handle := state.sessions[roleSessionID]
	delete(state.sessions, roleSessionID)
	state.mu.Unlock()
	if handle != nil && handle.engine != nil {
		handle.engine.ClearHistory()
	}
	return nil
}

// ── 座位作业面（D4）：goal 座位循环 = jobs.KindSeat 作业 ─────────────────────

const (
	// DefaultSeatJoinBudget 是座位作业的默认汇合预算（goal 域传 5 分钟；这里留一个
	// 同量级兜底，供调用方传 <=0 时使用）。
	DefaultSeatJoinBudget = 5 * time.Minute
	// seatJoinWaitStep 是单次 Fetch 的等待上限：作业面把单次等待 clamp 在
	// MaxWait（默认 60s），因此长预算必须由多次有界等待拼成，不能一次性要 5 分钟。
	seatJoinWaitStep = 30 * time.Second
	// seatKillGrace 是"尽力终止"的宽限：调用方 ctx 已经取消，Kill 不能再用它等。
	seatKillGrace = 5 * time.Second
)

// SeatJobsAssembled 报告座位作业面是否已装配（goal 域的装配探针，见
// goalCoordinatorDeps.SeatJobs）：seat 执行体与作业管理器都只在
// SetTeamworkBackend 里存在，没装配的宿主必须保持"端口为 nil"的现状语义。
func (r *Runtime) SeatJobsAssembled() bool {
	if r == nil {
		return false
	}
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	return r.teamworkJobs != nil
}

// teamworkManager 取作业面（未装配 → 显式报错，不静默降级）。
func (r *Runtime) teamworkManager() (jobs.Manager, error) {
	if r == nil {
		return nil, errors.New("teamwork: runtime 为空")
	}
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	if r.teamworkJobs == nil {
		return nil, errors.New("teamwork: 作业面未装配（缺 SetTeamworkBackend）")
	}
	return r.teamworkJobs, nil
}

// SetSeatRoundRunner 注入座位循环的执行侧实现（goal 域的实现者，见
// teamwork.SeatRoundRunner）。组合根在 initApplication 之后调一次。
//
// seat 执行体在 SetTeamworkBackend 时就注册好了（它拿着 Runtime 自身，每次 RunSeat
// 现取 runner），因此注册与注入的先后不成问题。
func (r *Runtime) SetSeatRoundRunner(runner teamwork.SeatRoundRunner) {
	if r == nil {
		return
	}
	r.seatRoundMu.Lock()
	r.seatRound = runner
	r.seatRoundMu.Unlock()
}

// seatRoundRunner 取座位循环的执行侧实现（未装配 → nil）。
func (r *Runtime) seatRoundRunner() teamwork.SeatRoundRunner {
	if r == nil {
		return nil
	}
	r.seatRoundMu.RLock()
	defer r.seatRoundMu.RUnlock()
	return r.seatRound
}

// DispatchSeat 实现 goal 域的 SeatJobs 派发侧：把一轮治理推进表达为一个
// jobs.KindSeat 作业（D4）。会话归属与工作正文一律走载荷——作业的执行 ctx 是
// jobs.Manager 从 Background 派生的，不带原调用的会话与 detail。
func (r *Runtime) DispatchSeat(ctx context.Context, sessionID, detail string) (string, error) {
	manager, err := r.teamworkManager()
	if err != nil {
		return "", err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", errors.New("teamwork: 座位作业缺会话归属")
	}
	payload, err := json.Marshal(teamwork.SeatRequest{SessionID: sessionID, Detail: detail})
	if err != nil {
		return "", fmt.Errorf("teamwork: 座位作业载荷编码失败: %w", err)
	}
	handle, err := manager.Dispatch(ctx, jobs.Spec{
		Kind:        teamwork.KindSeat,
		Scope:       jobs.Scope{Session: sessionID},
		Description: seatJobDescription(detail),
		Payload:     payload,
	})
	if err != nil {
		return "", fmt.Errorf("teamwork: 派发座位作业失败: %w", err)
	}
	return string(handle), nil
}

// JoinSeat 实现 goal 域的 SeatJobs 汇合侧：有界等到座位作业的终态，并按读数折回
// 结论（非 done ⇒ 本轮治理未完成，由 goal 域登记进 RoundError）。
//
// 有界：预算到点仍无终态就**尽力终止**该作业，并如实报回"仍在跑"——留一个还在跑的
// 座位循环去和下一个回合抢同一个 governor，不是安全降级。调用方 ctx 取消同理
// （返回错误 + 尽力 Kill）。
func (r *Runtime) JoinSeat(ctx context.Context, handle string, budget time.Duration) (dto.SeatJobOutcome, error) {
	manager, err := r.teamworkManager()
	if err != nil {
		return dto.SeatJobOutcome{}, err
	}
	if strings.TrimSpace(handle) == "" {
		return dto.SeatJobOutcome{}, errors.New("teamwork: 座位作业句柄为空")
	}
	if budget <= 0 {
		budget = DefaultSeatJoinBudget
	}
	deadline := time.Now().Add(budget)
	for {
		wait := time.Until(deadline)
		if wait <= 0 {
			return r.abandonSeat(manager, handle, budget), nil
		}
		if wait > seatJoinWaitStep {
			wait = seatJoinWaitStep
		}
		_, record, err := manager.Fetch(ctx, jobs.Handle(handle), jobs.FetchBudget{
			WaitMS: int(wait / time.Millisecond),
		})
		if err != nil {
			r.killSeatBestEffort(manager, handle)
			return dto.SeatJobOutcome{}, err
		}
		if record.State.Terminal() {
			return dto.SeatJobOutcome{
				State:    string(record.State),
				ExitCode: record.ExitCode,
				Summary:  record.Summary,
				Known:    true,
			}, nil
		}
	}
}

// abandonSeat 是"预算到点还没终态"的出口：先读一次读数（此时仍是 running），再尽力
// 终止作业，把读数如实交回（非 done ⇒ 本轮登记失败）。
func (r *Runtime) abandonSeat(manager jobs.Manager, handle string, budget time.Duration) dto.SeatJobOutcome {
	record, ok := manager.Observe(jobs.Handle(handle))
	r.killSeatBestEffort(manager, handle)
	if !ok {
		return dto.SeatJobOutcome{
			State:   string(jobs.StateKilled),
			Summary: fmt.Sprintf("goal 座位循环超过汇合预算 %s 未结束，作业句柄已不在册", budget),
		}
	}
	summary := strings.TrimSpace(record.Summary)
	if summary == "" {
		summary = fmt.Sprintf("goal 座位循环超过汇合预算 %s 未结束（state=%s）", budget, record.State)
	}
	return dto.SeatJobOutcome{
		State: string(record.State), ExitCode: record.ExitCode, Summary: summary, Known: true,
	}
}

// killSeatBestEffort 尽力终止一个座位作业。调用方的 ctx 可能已经取消（这正是走到
// 这里的原因之一），因此用一个独立的短宽限 ctx；Kill 的作用域判定按 ctx 解析，空
// 作用域是通配，所以这里不需要原会话。
func (r *Runtime) killSeatBestEffort(manager jobs.Manager, handle string) {
	if manager == nil {
		return
	}
	killCtx, cancel := context.WithTimeout(context.Background(), seatKillGrace)
	defer cancel()
	_ = manager.Kill(killCtx, jobs.Handle(handle))
}

// seatJobDescription 给座位作业一行可读标题：作业会活过派发它的那一轮，标题是它
// 唯一的说明（作业面要求 Description 非空）。
func seatJobDescription(detail string) string {
	const limit = 60
	line := strings.TrimSpace(detail)
	if index := strings.IndexAny(line, "\r\n"); index >= 0 {
		line = strings.TrimSpace(line[:index])
	}
	if runes := []rune(line); len(runes) > limit {
		line = string(runes[:limit]) + "…"
	}
	if line == "" {
		return "goal 座位循环"
	}
	return "goal 座位循环：" + line
}

// RunSeat 实现 teamwork.SeatRunner：执行体把座位作业折成 goal 域的 RunSeatRound
// 调用（会话归属与正文来自载荷，而不是作业的 ctx）。
//
// 未装配 runner（组合根没调 SetSeatRoundRunner）= **显式报错**：一个"什么都没发生
// 的成功作业"会把治理静默掉，比失败更糟。
func (r *Runtime) RunSeat(ctx context.Context, request teamwork.SeatRequest, sink jobs.Sink) error {
	if r == nil {
		return errors.New("teamwork: seat 执行体未装配（runtime 为空）")
	}
	runner := r.seatRoundRunner()
	if runner == nil {
		return errors.New("teamwork: seat 执行体未装配（缺 SeatRoundRunner）")
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		return errors.New("teamwork: 座位作业缺会话归属（载荷未带 session_id）")
	}
	var note func(string)
	if sink != nil {
		note = sink.Note
	}
	return runner.RunSeatRound(ctx, sessionID, request.Detail, note)
}

// worktreeDirty 报告工作区是否有未提交改动（git status --porcelain 非空）。
func worktreeDirty(root string) (bool, error) {
	out, err := worktree.GitRunner(root, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// workerRoundInput 组装 worker 这一轮的工作正文：把 goal 包成有界的 round_input，
// 明确"做完给结论与下一步"，与员工回合同一口径。
func workerRoundInput(request teamwork.WorkerRequest) string {
	goal := strings.TrimSpace(request.Goal)
	if goal == "" {
		goal = "（本轮没有新的工作正文）"
	}
	header := ""
	if stage := strings.TrimSpace(request.Stage); stage != "" {
		header = "<stage>" + stage + "</stage>\n"
	}
	return header + "<round_input>\n" + goal + "\n</round_input>\n\n" +
		"<task>\n以 " + request.Role + " 的身份完成这一轮：给出本轮的结论与下一步（≤800 字）。\n</task>"
}

func jsonReceipt(value map[string]any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
