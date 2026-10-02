package seelebridge

// runtime_teamwork.go — teamwork 编排面在 seelebridge 的落点：leader 的硬编排工具
// （team_plan / team_dispatch / team_join / team_milestone / team_retire / team_close /
// team_context）+ 框架通用管理工具 jobs_manage，以及它们背后的作业面与 Coordinator 组装。
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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/Seele/jobs/builtin"

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
	// Boards 是团队看板**存档**的读/写面（生产实现 = sessionstore 的
	// moduleBoardTeam，取用面 Router.BoardsFor）。可选：未注入 = 不刷新存档、
	// 读侧也无从恢复——"有就有、没有就是没装配"。
	Boards sessionstore.BoardRepository
	// JobOutputs 是 teammate 作业输出文件的产品面（S5 / §4.7 输出归属；生产实现 =
	// NewTeamworkJobOutputs）。可选：未注入 = Spec.OutputPath 留空，框架自建输出文件
	// 并按框架语义在销项 / 驱逐 / Close 时删除。
	JobOutputs teamwork.JobOutputs
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
		jobs.WithSessionResolver(func(ctx context.Context) string {
			return seeletelemetry.SessionIDFromContext(ctx)
		}),
		// 防 prune（S5）：把记录槽上限抬到"一支团队一开到底"的量级，而不是撞框架缺省的
		// 进程级兜底 256。prune 只逐**最老的终态行**、活着的行永不驱逐，所以撞上它时
		// 丢掉的恰好是"团队还在开、但某一轮已经跑完"的那些行——正是看板与 team_context
		// 的证据。框架的数是一个与团队寿命无关的进程级数字，产品这一侧的界由产品自己算。
		jobs.WithLimits(jobs.Limits{Records: teamworkJobRecordCeiling(backend.MaxTeammates)}),
	)
	if err != nil {
		return fmt.Errorf("teamwork: 装配 jobs.Manager 失败: %w", err)
	}
	// 作业事件流（Seelex 侧构建，见 jobs_events.go）：订阅 jobs.Events() 的变更
	// 信号，把在册作业的新状态按会话追加到事件库——框架侧不发事件（event.Sink 在
	// 构造期定不下会话、序号全局，只 append 不到尾部）。
	// 停机顺序 = 登记逆序：先登记 manager.Close、后登记 stream.close，于是停机时
	// 先停投影、再取消在途作业（不为停机合成一批 killed 事件）。
	stream := newJobsEventStream(manager, r.currentEventPersister)
	r.teamworkMu.Lock()
	r.teamworkBackend = &backend
	r.teamworkJobs = manager
	r.teamworkCoords = map[sessionstore.Key]*teamwork.Coordinator{}
	r.lifecycle = append(r.lifecycle, func() { _ = manager.Close(context.Background()) })
	r.lifecycle = append(r.lifecycle, stream.close)
	r.teamworkMu.Unlock()
	stream.start()
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

// teamworkRoundsPerMember 是"一名 teammate 在整队收口前最多派多少轮"的量级取用值。
//
// 它是**量级**而不是契约：一条 V 模型流水线里，一个角色通常只被派 1–3 轮（三层验证共用
// 一个 test_case 时是 3 轮）。取 64 是为了让"记录槽上限"落在数量级正确的一侧，而不是
// 精确预估——精确值要靠经验数据，而这不是一条需要精确的约束（下面那条只抬不降的钳制
// 保证它永远不会把上限压到框架缺省之下）。
const teamworkRoundsPerMember = 64

// teamworkJobRecordCeiling 是 teamwork 作业表的记录槽上限（jobs.Limits.Records，S5「防 prune」）。
//
// 为什么产品要自己给这个数：框架缺省（jobs.DefaultLimits().Records = 256）是**进程级兜底**，
// 与"一支团队从开工到收口能派多少次活"没有关系。prune 的判据是"表长 > 上限"、且只逐最老的
// **终态**行（活着的行永不驱逐），所以撞上它时丢掉的恰好是"团队还开着、但某一轮已经跑完"
// 的那些行——正是看板与 team_context 在收口之前要读的证据。
//
// 只抬不降：上限永不低于框架缺省。记录槽只保证"行**还在册**"，真正让正文不丢的是**输出归属**
// （Spec.OutputPath：文件归产品，销项 / 驱逐 / Close 都不删）——两条独立的路，别把后者
// 当成前者的替代（行被驱逐时读面就找不到那个句柄了）。
func teamworkJobRecordCeiling(maxTeammates int) int {
	if maxTeammates <= 0 {
		// 计划侧"不限制"（<=0）：按一名成员算，最终仍被框架缺省下限托住。
		maxTeammates = 1
	}
	ceiling := maxTeammates * teamworkRoundsPerMember
	if floor := jobs.DefaultLimits().Records; ceiling < floor {
		return floor
	}
	return ceiling
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
		Boards:       r,
		JobOutputs:   backend.JobOutputs,
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

// registerTeamworkTools 注册 leader 编排面（RegisterBuiltins 内调用；未装配 backend
// 时注册面为空——没有后端就不摆出一族永远报错的工具）。
//
// 工具集：team_plan / team_dispatch / team_join / team_milestone / team_retire（逐人一轮）
// + team_close（整队收口，唯一的回收点）+ team_context（成员上下文只读面）
// + jobs_manage（框架通用管理面）。
func (r *Runtime) registerTeamworkTools() {
	if !r.teamworkEnabled() {
		return
	}
	r.RegisterTool("team_plan", teamworkPlanDescription(), teamworkPlanSchema(), r.teamPlanHandler)
	r.RegisterTool("team_dispatch", teamworkDispatchDescription(), teamworkDispatchSchema(), r.teamDispatchHandler)
	r.RegisterTool("team_join", teamworkJoinDescription(), teamworkJoinSchema(), r.teamJoinHandler)
	r.RegisterTool("team_milestone", teamworkMilestoneDescription(), teamworkMilestoneSchema(), r.teamMilestoneHandler)
	r.RegisterTool("team_retire", teamworkRetireDescription(), teamworkRetireSchema(), r.teamRetireHandler)
	// team_close：整队**收口**的唯一入口。它与 team_retire 的区别只在"回收作业"这一处
	// （retire 不再回收，见 coordinator.retireSteps）：收口之前，作业正文一直留在册上。
	r.RegisterTool("team_close", teamworkCloseDescription(), teamworkCloseSchema(), r.teamCloseHandler)
	// team_context：成员工作上下文**读面**（要求③）。它只读、且正文走非消费读法
	// （Manager.Peek），因此与其余 team_* 工具不同：调用它不会改变任何事实。
	r.RegisterTool("team_context", teamworkContextDescription(), teamworkContextSchema(), r.teamworkContextHandler)
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
		TeamID     string                           `json:"team_id"`
		Version    int                              `json:"version"`
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
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
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
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
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
	// 汇合也是一次**审计追加**（Coordinator.Join 会记 join 事件），因此同样是看板的
	// 一个 update 时机（契约 §5 的表里有它）：不在这里失效缓存，面板就要等到下一次
	// team_plan/dispatch/milestone/retire 才看得到这次汇合（"漏一处就是静默陈旧"）。
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
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
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
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
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{
		"ok": true, "role": raw.Role,
		"detail": "释放 worktree → 清会话内容 → 保在线（作业不在这里回收：它活到 team_close）",
	})
}

// teamCloseHandler 收口整支团队（team_close）：逐在编成员走同一套退场四步（回收统一
// 收口到这一处）→ 封板团队看板（closed / team.close）→ 计划标 closed → 落一条 close 审计。
//
// 收口不接受参数：收口的是"这支团队"，不是某一个人（逐人退场是 team_retire）。
// 幂等：已收口的团队第二次调用返回 already_closed=true，**不重复封板、不重复落审计**。
//
// 这里刻意**不**再调 archiveTeamBoard：收口路径自己已经封板（Coordinator.Close →
// BoardCloser），而刷新逻辑在计划 closed 时会走封板分支——两次写同一件事没有必要。
func (r *Runtime) teamCloseHandler(ctx context.Context, _ string) (string, error) {
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	alreadyClosed, err := coordinator.Close(ctx)
	if err != nil {
		return "", fmt.Errorf("team_close: %w", err)
	}
	r.invalidateTeamworkBoard()
	return jsonReceipt(map[string]any{
		"ok": true, "already_closed": alreadyClosed,
		"detail": "逐在编成员：回收作业 → 释放 worktree → 清会话内容；然后封板看板（team.close）",
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
	r.bindWorkerProjectRoot(mainSessionID, request.RoleSessionID, request.Worktree)
	maxLoops := request.MaxTurns
	if maxLoops <= 0 {
		maxLoops = roleTurnMaxLoops
	}
	output, err := r.runRoleRound(ctx, r.workerRoleRoundSpec(request, mainSessionID, maxLoops))
	if err != nil {
		// 产品自有输出文件时，失败正文也得落进这个文件：框架在这种形态下**不写**
		// （externalOutput.write 是空操作），不写就等于"这一轮出过错"这件事在正文里
		// 没有痕迹——而正文正是收口时被读的那份证据。
		if path := strings.TrimSpace(request.OutputPath); path != "" {
			_ = r.writeWorkerOutput(path, "worker 回合失败："+err.Error()+"\n")
		}
		return err
	}
	if path := strings.TrimSpace(request.OutputPath); path != "" {
		// 归产品：执行体自己写（框架不写这个文件）。写失败上抛而不是退回 sink.Note——
		// 那个 sink 在这种形态下是空操作，退回就是静默丢正文。
		return r.writeWorkerOutput(path, output)
	}
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		sink.Note(trimmed + "\n")
	}
	return nil
}

// workerRoleRoundSpec 组装一个 teammate 作业回合的执行面入参。
//
// 抽成方法而不是内联：`WorkScope` 这一格是**实时观察面的唯一入口**（见 roleWorkScope）——
// 漏掉它，员工在做工时的工具活动就一条也发不出去，前端又回到"等这一轮跑完才看得到结果"
// 的老现场，而那种回归在类型上是合法的（空 WorkScope 只是"不发活动"），只能靠用例钉。
func (r *Runtime) workerRoleRoundSpec(request teamwork.WorkerRequest, mainSessionID string, maxLoops int) roleRoundSpec {
	return roleRoundSpec{
		MainSessionID:    mainSessionID,
		RoleName:         request.Role,
		RoleSessionID:    request.RoleSessionID,
		ToolsPolicy:      request.ToolsPolicy,
		PermissionGroups: request.PermissionGroups,
		SystemPrompt:     r.roleTurnSystemPrompt(request.Role),
		Input:            workerRoundInput(request),
		MaxLoops:         maxLoops,
		WorkScope: roleWorkScope{
			MainSessionID: mainSessionID,
			RoleName:      request.Role,
			RoleSessionID: request.RoleSessionID,
		},
	}
}

// writeWorkerOutput 把本轮正文写进**产品自有**的输出文件（jobs.Spec.OutputPath 形态）。
//
// 写经**产品面**（teamwork.JobOutputs.WriteJobOutput）而不是直接落盘：产品面把"写"与
// "收口清目录"放在同一把锁上串行，并作废已清掉那一批的落点——被取消的执行体最后一次写
// 因此不会在清目录之后凭空造出一个残文件（S5 残边，devlog §4.3）。
//
// 组装矛盾（带落点却没装配产品面）时不静默丢正文：退回直接落盘，宁可少一道串行也不丢证据。
func (r *Runtime) writeWorkerOutput(path, text string) error {
	if outputs := r.teamworkJobOutputs(); outputs != nil {
		return outputs.WriteJobOutput(path, text)
	}
	return writeWorkerOutputFile(path, text)
}

// teamworkJobOutputs 取当前装配的产品作业输出面（未装配 = nil）。
func (r *Runtime) teamworkJobOutputs() teamwork.JobOutputs {
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	if r.teamworkBackend == nil {
		return nil
	}
	return r.teamworkBackend.JobOutputs
}

// writeWorkerOutputFile 是未装配产品输出面时的直接落盘（带落点却未装配 = 组装矛盾，
// 这条分支只在那种情况下兜底）。目录先建：路径由 TeamworkJobOutputs 分配
// （会话 teamwork/jobs），重启后目录可能已被清掉，而"上次收口清过目录"不该让这一次派发失败。
func writeWorkerOutputFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("teamwork: 创建作业输出目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return fmt.Errorf("teamwork: 写作业输出失败: %w", err)
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

// 座位作业面（D4：goal 座位循环 = jobs.KindSeat 作业）已随席位轮转退场删除
// （2026-10-01 阶段三 W3）。teamwork 作业面现在只承载 worker 作业。

// SetSeatRoundRunner / seatRoundRunner / DispatchSeat / JoinSeat / abandonSeat /
// killSeatBestEffort / seatJobDescription / RunSeat 已随席位轮转退场删除
// （2026-10-01 阶段三 W3）。

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
