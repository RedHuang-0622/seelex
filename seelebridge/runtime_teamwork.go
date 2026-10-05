package seelebridge

// runtime_teamwork.go — teamwork 编排面在 seelebridge 的落点：leader 的硬编排工具
// （team_plan / team_dispatch / team_join / team_milestone / team_close /
// team_context）+ 框架通用管理工具 jobs_manage，以及它们背后的作业面与 Coordinator 组装。
//
// 分工（docs/arch/teamwork-leader-worker-architecture.md §2 / §4.5 / D1）：
//   - **作业面**归 Seele 的 jobs 根能力（契约 + Manager + jobs_manage）；
//   - **硬编排**归 seelebridge/teamwork（计划 + 派发 + 汇合 + 里程碑 + 整队收口）；
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
		jobs.WithExecutor(teamwork.WorkerExecutor(r, r, backend.MaxTurns)),
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
	//
	// **信号口要先扇出**（teamwork_job_signals.go）：上游 jobs.Manager.Events() 是
	// 容量 1 的单接收者通道，而现在有两个读侧动作要跟它走（事件投影 + 终态触发回合）。
	// 这里读一次、广播出去，两个订阅者各一条通道；"取回"仍只从读面走（信号不推数据）。
	signals := newTeamworkJobSignals()
	signals.start(manager.Events())
	appJobEvents := signals.subscribe()
	stream := newJobsEventStream(manager, r.currentEventPersister, signals.subscribe())
	r.teamworkMu.Lock()
	r.teamworkBackend = &backend
	r.teamworkJobs = manager
	r.teamworkCoords = map[sessionstore.Key]*teamwork.Coordinator{}
	r.teamworkJobSignals = signals
	r.teamworkJobEvents = appJobEvents
	r.lifecycle = append(r.lifecycle, func() { _ = manager.Close(context.Background()) })
	r.lifecycle = append(r.lifecycle, stream.close)
	r.lifecycle = append(r.lifecycle, signals.close)
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
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, errors.New("teamwork: 当前调用没有会话归属（team_* 工具必须在会话回合内调用）")
	}
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.KeyFor == nil {
		return nil, errors.New("teamwork: leader 编排未装配（缺 SetTeamworkBackend）")
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok || strings.TrimSpace(key.ProjectID) == "" || strings.TrimSpace(key.SessionID) == "" {
		return nil, fmt.Errorf("teamwork: 无法解析会话 %q 的作用域键（项目/会话）", sessionID)
	}
	return r.coordinatorForKey(key)
}

// coordinatorForKey 取（必要时建）某个会话作用域键对应的 Coordinator。
func (r *Runtime) coordinatorForKey(key sessionstore.Key) (*teamwork.Coordinator, error) {
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	backend, manager := r.teamworkBackend, r.teamworkJobs
	if backend == nil || manager == nil {
		return nil, errors.New("teamwork: leader 编排未装配（缺 SetTeamworkBackend）")
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
		Spaces:       r,
		Teammates:    r,
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
// 工具集：team_plan / team_dispatch / team_join / team_milestone / team_close（整队收口，
// 唯一的回收点）+ team_context（成员上下文只读面）+ jobs_manage（框架通用管理面）。
func (r *Runtime) registerTeamworkTools() {
	if !r.teamworkEnabled() {
		return
	}
	r.RegisterTool("team_plan", teamworkPlanDescription(), teamworkPlanSchema(), r.teamPlanHandler)
	r.RegisterTool("team_dispatch", teamworkDispatchDescription(), teamworkDispatchSchema(), r.teamDispatchHandler)
	r.RegisterTool("team_join", teamworkJoinDescription(), teamworkJoinSchema(), r.teamJoinHandler)
	r.RegisterTool("team_milestone", teamworkMilestoneDescription(), teamworkMilestoneSchema(), r.teamMilestoneHandler)
	// team_close：整队**收口**的唯一入口：作业回收、现场拆除、会话内容清空都落在这一处
	// （唯一的回收实现 = coordinator.closeStepsLocked + releaseAllItems；逐人退场那条
	// 口径已整条删除，2026-10-06）。
	r.RegisterTool("team_close", teamworkCloseDescription(), teamworkCloseSchema(), r.teamCloseHandler)
	// team_context：成员工作上下文**读面**（要求③）。它只读、且正文走非消费读法
	// （Manager.Peek），因此与其余 team_* 工具不同：调用它不会改变任何事实。
	r.RegisterTool("team_context", teamworkContextDescription(), teamworkContextSchema(), r.teamworkContextHandler)
	// Work Item 口径的 leader 工具面（2026-10-03）：排活（按里程碑）/ 调整未开始项 /
	// 验收通过 / 判失败 / 中断恢复 / 读工作项。
	r.RegisterTool("team_work", teamworkWorkDescription(), teamworkWorkSchema(), r.teamWorkHandler)
	r.RegisterTool("team_item", teamworkItemDescription(), teamworkItemSchema(), r.teamItemHandler)
	r.RegisterTool("team_accept", teamworkAcceptDescription(), teamworkAcceptSchema(), r.teamAcceptHandler)
	r.RegisterTool("team_fail", teamworkFailDescription(), teamworkFailSchema(), r.teamFailHandler)
	r.RegisterTool("team_recover", teamworkRecoverDescription(), teamworkRecoverSchema(), r.teamRecoverHandler)
	r.RegisterTool("team_items", teamworkItemsDescription(), teamworkItemsSchema(), r.teamItemsHandler)
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
		Members: raw.Members, Milestones: raw.Milestones,
	}
	// 插件装配（能力轴）在这里做两道**显式拒绝**——只有这一层手里同时有插件定义与
	// 配置上限（validateMemberPlugins）：
	//   ① 未知名：不静默忽略（静默忽略会让 leader 以为装上了、员工那头一个能力也
	//      没有，而且要到员工跑完才发现）；
	//   ② 超过每会话上限（limits.plugins.per_teammate）：不静默截断（截断会把
	//      "我声明了 5 个"悄悄变成"装了 3 个"）。
	// 语法规整（去空白/去空项/**重复显式拒绝**）走 dto.NormalizePlugins：与写入侧
	// （RoleSpec）同一份口径，不在这里另写一遍。
	//
	// 精选目录闸（**先做**，见 runtime_teamwork_curated.go）：名字**未定义**时多问一句
	// "它是谁"——pending 候选（路线图）要点名上游来源，谁都不认识要说清"不在已装插件、
	// 也不在精选目录"，精选目录没读到要出声（读不到不得静默当空目录）。已装插件不问、
	// 行为不变；语法与上限仍由 validateMemberPlugins 报。
	if err := r.rejectUnassembledMemberPlugins(plan.Members); err != nil {
		return "", fmt.Errorf("team_plan: %w", err)
	}
	if err := validateMemberPlugins(plan.Members, r.maxPluginsPerTeammate(), r.plugins.Defined); err != nil {
		return "", fmt.Errorf("team_plan: %w", err)
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
		"ok": true, "team_id": plan.TeamID,
		"members": len(plan.Members), "milestones": len(plan.Milestones),
		// 装配回执**必须带读数**（契约 7 / 黄牌不拒）：逐成员的集合 + 技能目录字节
		// 与 token 估算 + 黄牌；空集显式写成 inherit-host（"不覆盖"不靠字段缺失暗示）。
		"plugin_limit_per_teammate": r.maxPluginsPerTeammate(),
		"assemblies":                r.assemblyViews(plan.Members),
	})
}

// dispatchAssemblyReading 给**派发回执**补一条装配读数（与运行面同一判据：见
// pluginFaceJudgement）。
//
// 为什么派发这一跳也要读数：`team_plan` 的读数只能表达"声明当时合法"——未知名在那一步
// 就被**显式拒绝**了，所以"声明过、之后被撤销"这种失灵在计划的回执里永远不会出现。
// 而失灵的后果正落在派发之后的运行面上（空工具面）。于是派发回执是唯一能把失灵**说出来**
// 的回执：leader 因此看到的是"声明 docs，现已失灵，工具面为空"，而不是等员工跑完才发现
// 一个工具都没有（那时已经烧掉一整轮）。
//
// 失灵**不拒绝派发**：排活/派发是既定事实，失灵是运行面的读数（拒绝会把"插件被撤"变成
// 另一个语义完全不同的错误）。
func (r *Runtime) dispatchAssemblyReading(ctx context.Context, coordinator *teamwork.Coordinator, role string) *rolePluginAssemblyView {
	if r == nil || coordinator == nil {
		return nil
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return nil
	}
	plan, err := coordinator.Plan(ctx)
	if err != nil {
		return nil
	}
	for _, member := range plan.Members {
		if member.Role != role {
			continue
		}
		views := r.assemblyViews([]sessionstore.TeamworkMember{member})
		if len(views) == 0 {
			return nil
		}
		return &views[0]
	}
	return nil
}

// dispatchItemRole 在计划里按工作项 id 找它的执行角色（派发回执的读数要按**这个工作项**
// 的执行人算，而不是按 leader 在参数里写的角色）。
func dispatchItemRole(ctx context.Context, coordinator *teamwork.Coordinator, itemID string) string {
	if coordinator == nil {
		return ""
	}
	plan, err := coordinator.Plan(ctx)
	if err != nil {
		return ""
	}
	itemID = strings.TrimSpace(itemID)
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if strings.TrimSpace(item.ID) == itemID {
				return item.Role
			}
		}
	}
	return ""
}

func (r *Runtime) teamDispatchHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Role string `json:"role"`
		Item string `json:"item"`
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_dispatch: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	// Work Item 口径：给了 item 就按甘特节点派发（屏障 + 依赖 + 一 Work Item 一套
	// 会话与 worktree 都归编排面管）。没给就是 teammate 级的老口径。
	if itemID := strings.TrimSpace(raw.Item); itemID != "" {
		handle, err := coordinator.DispatchItem(ctx, itemID)
		if err != nil {
			return "", fmt.Errorf("team_dispatch: %w", err)
		}
		r.invalidateTeamworkBoard()
		r.archiveTeamBoard(ctx)
		return jsonReceipt(map[string]any{
			"ok": true, "handle": string(handle), "item": itemID,
			// 装配读数：这一轮真的会按哪份装配跑（**失灵在这里被说出来**——plan 的回执
			// 表达不了它，见 dispatchAssemblyReading）。
			"plugin_face": r.dispatchAssemblyReading(ctx, coordinator, dispatchItemRole(ctx, coordinator, itemID)),
			"hint":        "受理回执即返回，不等待：继续你的关键路径，需要时用 jobs_manage(op=observe/fetch) 或 team_join 观察。",
		})
	}
	role := strings.TrimSpace(raw.Role)
	handle, err := coordinator.Dispatch(ctx, role, raw.Goal)
	if err != nil {
		return "", fmt.Errorf("team_dispatch: %w", err)
	}
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{
		"ok": true, "handle": string(handle),
		"plugin_face": r.dispatchAssemblyReading(ctx, coordinator, role),
		"hint":        "受理回执即返回，不等待：继续你的关键路径，需要时用 jobs_manage(op=observe/fetch) 或 team_join 观察。",
	})
}

// teamWorkHandler 给一个里程碑**排活**（分里程碑排活：依赖未完成的里程碑会被拒）。
func (r *Runtime) teamWorkHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Milestone string `json:"milestone"`
		Items     []struct {
			ID          string   `json:"id"`
			Role        string   `json:"role"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Goal        string   `json:"goal"`
			DependsOn   []string `json:"depends_on"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_work: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	specs := make([]teamwork.WorkItemSpec, 0, len(raw.Items))
	for _, item := range raw.Items {
		specs = append(specs, teamwork.WorkItemSpec{
			ID: item.ID, Role: item.Role, Name: item.Name,
			Description: item.Description, Goal: item.Goal, DependsOn: item.DependsOn,
		})
	}
	if err := coordinator.PlanMilestone(ctx, strings.TrimSpace(raw.Milestone), specs); err != nil {
		return "", fmt.Errorf("team_work: %w", err)
	}
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{"ok": true, "milestone": raw.Milestone, "items": len(specs)})
}

// teamItemHandler 调整一条**尚未开始**的工作项。
func (r *Runtime) teamItemHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		ID          string   `json:"id"`
		Role        string   `json:"role"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Goal        string   `json:"goal"`
		DependsOn   []string `json:"depends_on"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_item: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	if err := coordinator.AdjustItem(ctx, strings.TrimSpace(raw.ID), teamwork.WorkItemSpec{
		Role: raw.Role, Name: raw.Name, Description: raw.Description,
		Goal: raw.Goal, DependsOn: raw.DependsOn,
	}); err != nil {
		return "", fmt.Errorf("team_item: %w", err)
	}
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{"ok": true, "id": raw.ID})
}

// teamAcceptHandler 是 leader 的**验收通过**：工作项 → done，并结束这件事的执行隔离。
func (r *Runtime) teamAcceptHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		ID   string `json:"id"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_accept: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	if err := coordinator.AcceptItem(ctx, strings.TrimSpace(raw.ID), raw.Note); err != nil {
		return "", fmt.Errorf("team_accept: %w", err)
	}
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{"ok": true, "id": raw.ID})
}

// teamFailHandler 判定一件工作不通过（现场与记忆都留着，可重派）。
func (r *Runtime) teamFailHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		ID   string `json:"id"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_fail: 参数解析失败: %w", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	if err := coordinator.FailItem(ctx, strings.TrimSpace(raw.ID), raw.Note); err != nil {
		return "", fmt.Errorf("team_fail: %w", err)
	}
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{"ok": true, "id": raw.ID})
}

// teamRecoverHandler 做中断恢复（额度中断 / 重启）：读回计划与绑定，把"句柄已作废、
// 可重派"显式化。它**不动**任何会话与工作区——记忆与现场都要留着。
//
// 同一次调用还做契约 `Lifecycle.Recover` 那一半（见 workunit_team.go 的 RecoverTeamworkUnits）：
// 认领团队现场（**在 Prune 之前**）→ 读回 teammate 单元的会话记录 → 把"记录说在跑、
// 本进程已无它的执行面"判成**中断** → 给该角色下一次装配注入恢复说明（一次性读完即消）。
// 两半共用一次读：`resume` 就是各成员"回到哪一步"的读数，不再另取一份进度真相。
func (r *Runtime) teamRecoverHandler(ctx context.Context, _ string) (string, error) {
	if _, err := r.coordinatorFor(ctx); err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	recovery, err := r.RecoverTeamworkUnits(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("team_recover: %w", err)
	}
	r.invalidateTeamworkBoard()
	r.archiveTeamBoard(ctx)
	return jsonReceipt(map[string]any{
		"ok": true, "report": recovery.Report,
		// 契约侧的读数：认领回来的现场数 / 回灌回来的会话数 / 中断清单（记录说在跑、
		// 本进程已无执行面 → 可重派或待人工处置）。恢复说明已经记在该角色会话名下，
		// 等它下一次装配时读走。
		"resume": recovery.Resume,
		"hint":   "现场与会话都不动：重派同一工作项会复用它的会话号（记忆建在这上面）。",
	})
}

// teamItemsHandler 读回全部工作项（甘特图的数据面）。
func (r *Runtime) teamItemsHandler(ctx context.Context, _ string) (string, error) {
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	items, err := coordinator.Items(ctx)
	if err != nil {
		return "", fmt.Errorf("team_items: %w", err)
	}
	r.invalidateTeamworkBoard()
	return jsonReceipt(map[string]any{"ok": true, "items": items})
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

// teamCloseHandler 收口整支团队（team_close）：逐在编成员走同一套收口四步（作业回收、
// 现场拆除、会话内容清空统一落在这一处）→ 封板团队看板（closed / team.close）→ 计划标
// closed → 落一条 close 审计。
//
// 收口不接受参数：收口的是"这支团队"，不是某一个人（逐人退场那条口径已整条删除，
// 一轮的结束只有这一个入口）。
// 幂等：已收口的团队第二次调用返回 already_closed=true，**不重复封板、不重复落审计**。
//
// 这里刻意**不**再调 archiveTeamBoard：收口路径自己已经封板（Coordinator.Close →
// BoardCloser），而刷新逻辑在计划 closed 时会走封板分支——两次写同一件事没有必要。
func (r *Runtime) teamCloseHandler(ctx context.Context, _ string) (string, error) {
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	alreadyClosed, err := coordinator.Close(ctx)
	if err != nil {
		return "", fmt.Errorf("team_close: %w", err)
	}
	// 收口 = 这支团队唯一"不再算残留"的时刻：现场拆了、会话内容清了，那么"这件事跑到哪"
	// 的单元记录也没有事实可指（契约的会话面在收口时清，见 workunit_team.go）。
	cleared := r.clearTeamUnitRecords(sessionID)
	r.invalidateTeamworkBoard()
	return jsonReceipt(map[string]any{
		"ok": true, "already_closed": alreadyClosed, "cleared_records": cleared,
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
	// 运行期落盘（契约 workunit 的会话面）：这一轮跑到哪、现场在哪。角色会话此前**只在
	// 内存**（重启即失忆），记录是重启回灌唯一的依据——所以它在**开跑之前**写。
	teamKey := teamUnitKeyFor(request)
	r.markTeamUnitRunning(mainSessionID, teamKey)
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
		r.recordTeamUnitRoundOutput(mainSessionID, teamKey, err.Error())
		return err
	}
	r.recordTeamUnitRoundOutput(mainSessionID, teamKey, output)
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
//
// 系统提示里还夹一件事：**中断恢复说明**（teamResume，见 workunit_team.go）。它与子代理
// 的 `SubagentResumeNote` 同形同语义——system 事实、只进被执行单元自己的上下文、
// 读完即消；落点不同（subagent 在节点装配 PromptBlocks 时读，teammate 在角色装配这里读），
// 但两层的文案来自同一份构建器 `workunit.RecoveryNote`（前缀族因此是同一种东西）。
func (r *Runtime) workerRoleRoundSpec(request teamwork.WorkerRequest, mainSessionID string, maxLoops int) roleRoundSpec {
	systemPrompt := r.roleTurnSystemPrompt(request.Role)
	if note := r.consumeTeamResumeNote(request.RoleSessionID); note != "" {
		systemPrompt = systemPrompt + "\n\n" + note
	}
	return roleRoundSpec{
		MainSessionID:    mainSessionID,
		RoleName:         request.Role,
		RoleSessionID:    request.RoleSessionID,
		ToolsPolicy:      request.ToolsPolicy,
		PermissionGroups: request.PermissionGroups,
		Plugins:          request.Plugins,
		SystemPrompt:     systemPrompt,
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
//
// **两处命名必须是同一个键**：进计划与账本的 Worktree 是**指派名**（带 `seelex/`
// 分支前缀，见 teamwork.WorkItemWorktreeName），而 worktree 管理器的注册键是**裸
// nodeID**（branch = "seelex/" + nodeID，见 workItemNodeID）。拿指派名直接查注册表
// 必然查空 → 静默回退主会话项目根，teammate 的 read_file/write_file/bash 因此全落在
// main 上（"worktree 的归属总是去到 main"）。去前缀的换算只有 workItemNodeID 一处，
// 这里复用它，不重新拼命名。
func (r *Runtime) bindWorkerProjectRoot(mainSessionID, roleSessionID, worktreeName string) {
	if r == nil || r.projectScope == nil || strings.TrimSpace(roleSessionID) == "" {
		return
	}
	root := ""
	if name := workItemNodeID(worktreeName); name != "" && r.worktreeMgr != nil {
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

// ReleaseWorkspace 实现 teamwork.WorkspaceReleaser：释放一个 teammate 的**角色级**
// 现场（teammate 级 nodeID = 角色名，指派名 `seelex/<role>`）。
//
// 脏工作区**显式报错**（ErrUncommittedChanges 语义）而不是静默丢弃——现场是人的
// 资产，框架不替人做"丢还是留"的决定。无现场 = 无可释放（幂等）。
//
// 换算与建现场**同一个键**：现场注册在裸 nodeID 下（teammate 级 = 角色名；去
// `seelex/` 前缀的换算见 workItemNodeID），所以这里拿角色名查注册表是对的。过去这
// 条路一直是空操作，根因是角色级现场从来没被建出来（F2）——"查不到现场"因此成了
// 唯一分支。Work Item 级现场（`seelex/<role>-<item>`）不在这里释放：它们的绑定在
// 账本里，按账本逐个释放（见 teamwork 的 releaseTeammateScenes）。
func (r *Runtime) ReleaseWorkspace(ctx context.Context, role string) error {
	if r == nil || r.worktreeMgr == nil {
		return errors.New("teamwork: 释放工作区需要 worktree 管理器（未装配）")
	}
	nodeID := strings.TrimSpace(role)
	if nodeID == "" {
		return nil
	}
	info, ok := r.worktreeMgr.Info(nodeID)
	if !ok {
		return nil // 无现场：无可释放（幂等）
	}
	if path := strings.TrimSpace(info.Path); path != "" {
		dirty, err := worktreeDirty(path)
		if err != nil {
			return fmt.Errorf("teamwork: 释放工作区前检查失败: %w", err)
		}
		if dirty {
			return fmt.Errorf("teamwork: teammate %q 的工作区有未提交改动（%s）: %w", role, path, worktree.ErrUncommittedChanges)
		}
	}
	// 清目录 + 删分支：复用 worktree 既有的**幂等**清理（目录/分支已不在 = 已释放），
	// 编排面不重写 git 调用；随后清注册表。
	if root := r.workspaceRootFor(seeletelemetry.SessionIDFromContext(ctx)); root != "" {
		if err := worktree.CleanupWorktree(root, &worktree.NodeWorktree{Path: info.Path, Branch: info.Branch}); err != nil {
			return fmt.Errorf("teamwork: 释放 teammate %q 的工作区失败（%s）: %w", role, info.Path, err)
		}
	}
	r.worktreeMgr.Release(nodeID)
	return nil
}

// ResetSession 实现 teamwork.SessionResetter：清一个角色会话的**记录内容**
// （工作历史），保留在编。角色会话是**进程内**执行面（刻意不接 DurableHistory），
// 因此清内存历史 + 落掉引擎槽即"内容已清"；下一次派发以干净上下文重开。
//
// 同时丢掉这个会话还没被读走的恢复说明（teamResume）：说明的落点就是这个会话，
// 会话没了它没有落点。**单元记录**的清点不在这里：那需要主会话号（存储键），而这条
// 端口只认角色会话号——记录由两处清，都是"这份现场被回收"的那一刻：
// per-unit 的 `teamUnit.Reclaim`，与整队收口的 `Runtime.clearTeamUnitRecords`。
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
	r.clearTeamResumeNote(roleSessionID)
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
	// 归属标签只认里程碑（阶段口径已退场，2026-10-04）：worker 因此永远看得到
	// "我这一轮是哪一步的活"，而不是一个已经不存在的阶段 id。
	if milestone := strings.TrimSpace(request.Milestone); milestone != "" {
		header = "<milestone>" + milestone + "</milestone>\n"
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
