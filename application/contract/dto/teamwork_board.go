package dto

// teamwork_board.go — 团队看板（Teamwork）的只读投影形状。
//
// 契约：docs/arch/team-board-gui-tui-contract.md §2。
//
// 生态位：`sessionstore.TeamworkPlan`（计划，整份替换）+ `sessionstore.TeamworkEvent`
// （审计，只追加）+ `jobs.Manager` 的作业行（内存态）三份事实，由 seelebridge 组装成
// 这里的 DTO，经 SessionRuntime 下发给 GUI / TUI。**只有后端 → 前端的单向投影**：
// 前端没有任何写入口，渲染结果不回写。
//
// 形状刻意与渲染件 `gui/frontend/dist/team-board-view.js` 的入参同名同义
// （plan.members / plan.milestones / plan.work_items / jobs / events）——前端只做搬运，
// 不在搬运里做判定；任何判定（里程碑状态折算、拓扑排序、层号）都归渲染件的纯函数。
//
// **没有 stages**（2026-10-04）：阶段口径整条退场，顺序的唯一事实是里程碑屏障
// （milestones[].depends_on）+ 里程碑内的工作项 DAG（work_items[].depends_on）。

// TeamworkBoardView 是某会话的团队看板只读投影。
//
// nil 或 milestones/work_items 都为空 = 该会话没有团队计划：GUI 与 TUI **都不渲染空壳**
// （口径同目标看板，「结束就是没有了」）。
type TeamworkBoardView struct {
	TeamID  string `json:"team_id,omitempty"`
	Version int    `json:"version,omitempty"`
	// MaxMembers 是产品级在编上限（TeamworkBackend.MaxTeammates；0 = 不限制）。
	// 它进投影的理由：看板要说"在编 n/max"是**超出上限**还是正常——没有上限这一份，
	// 前端只能显示"在编 n"，把一条产品约束降级成一个不知道好坏的数。
	MaxMembers int `json:"max_members,omitempty"`
	// Stale 是 **jobs I-4 的显式化**：句柄只在内存（进程重启即作废），当计划里残留着
	// 句柄投影、而本进程的句柄表里查不到它时置真。它只影响一行提示，不改变任何判定。
	Stale bool `json:"stale,omitempty"`
	// Recovered 是**存档兜底**的显式化：活体给不出看板（无计划 / 计划没有可看的编排）时
	// 由存档快照恢复（§6 重启恢复）。前端据此说明"这是上一次的存档，不是活体事实"。
	Recovered bool `json:"recovered,omitempty"`
	// State / ClosedAt / ClosedReason 是**整队收口**（team_close）的三字段。
	//
	// closed 事实的**域内权威在计划**（sessionstore.TeamworkPlan.State，U3 裁决）。它们
	// 进投影只服务**存档载荷**（封板时那一版与下发同形）：已经收口的计划在下发侧整块
	// 退场（TeamworkBoardSnapshot 对它返回 nil——"结束就是没有了"），所以消费方在活体
	// 路径上不会看到非空 State。存档侧同样只恢复 state=active，两份读侧口径一致。
	State        string                  `json:"state,omitempty"`
	ClosedAt     int64                   `json:"closed_at,omitempty"`
	ClosedReason string                  `json:"closed_reason,omitempty"`
	Members      []TeamworkMemberView    `json:"members,omitempty"`
	Milestones   []TeamworkMilestoneView `json:"milestones,omitempty"`
	// WorkItems 是全部工作项的**扁平**投影（甘特图的数据面；按 milestone 分组由
	// 消费方做）。依赖：里程碑内用 item.depends_on，里程碑之间用 milestones[].depends_on。
	WorkItems []TeamworkWorkItemView `json:"work_items,omitempty"`
	Jobs      []TeamworkJobView      `json:"jobs,omitempty"`
	Events    []TeamworkEventView    `json:"events,omitempty"`
}

// TeamworkMemberView 是一个在编 teammate。
//
// Queue 是它负责的**工作项名称队列**（按里程碑顺序、里程碑内按排活顺序）：看板要回答
// "这个人手上还有什么"，而不是让读的人自己把工作项按角色再分一次组。
type TeamworkMemberView struct {
	Role          string `json:"role"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	Worktree      string `json:"worktree,omitempty"`
	ToolsPolicy   string `json:"tools_policy,omitempty"`
	// Status 是这个人的实时状态，**只有两个值**（2026-10-04 用户口径）：
	// `running`（此刻手上真有在跑的工作项）/ `free`（没有在跑的；含"活干完了等验收"）。
	// 不存在 done：teammate 的状态回答的是"这个人此刻在不在干活"，而"这件事做完没"
	// 是工作项自己的状态（status 列）——两个问题两个字段，不合并。
	Status string `json:"status,omitempty"`
	// CurrentSessionID / CurrentWorkItem 是这位 teammate **此刻那件事的会话**
	// （2026-10-04 用户口径：看板点开的要是"当前的 teammate 的会话"，不是员工的
	// 历史会话）。
	//
	// 语义：优先"在跑的工作项"，其次"等验收的"，都没有就退到这位最近一次开工的
	// 工作项；一个都没开过 → 两个字段都空（前端退回角色会话，并按文案说明）。
	// 这条区分是必要的：RoleSessionID 是**员工的长期会话**（跨工作项、跨轮次），
	// 而一个 Work Item 有**自己的会话**（一 Work Item 一套 Session + worktree）。
	CurrentSessionID string `json:"current_session_id,omitempty"`
	CurrentWorkItem  string `json:"current_work_item,omitempty"`
	// Queue 是"这个人负责、且还没完成"的工作项名称（已销项的不再占队列）。
	Queue []string `json:"queue,omitempty"`
	// Messages 是尾插进这个人消息队列的回执（有界：近若干条，最近的在后面）。
	// 它是"leader 不主动问也能看到结论"的读面——作业是后台跑的，结果不 push 进忙会话。
	Messages []TeamworkTeammateMessageView `json:"messages,omitempty"`
}

// TeamworkMilestoneView 是一个里程碑；Status 为空 = pending（与 sessionstore 同口径）。
//
// DependsOn 是里程碑之间的**屏障**（串行）。工作项**不嵌在这里**：它们走顶层
// WorkItems 的扁平投影（每条带 milestone），消费方按 milestone 分组渲染——两处各放
// 一份就是两份事实，迟早不一致。
type TeamworkMilestoneView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	Required  []string `json:"required,omitempty"`
	Status    string   `json:"status,omitempty"`
	Content   string   `json:"content,omitempty"`
}

// TeamworkWorkItemView 是一个工作项（甘特图的节点）。
//
// Live / Interrupted 是**读出来的事实**而不是状态字段：Live = 这件事现在真的有一份
// 未释放的工作区绑定；Interrupted = 状态说在跑、而本进程的作业表里查不到它的句柄
// （jobs I-4：句柄只在内存）——它不是错误，是"可以重派"的信号。
type TeamworkWorkItemView struct {
	ID          string   `json:"id"`
	Milestone   string   `json:"milestone,omitempty"`
	Role        string   `json:"role,omitempty"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Goal        string   `json:"goal,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Status      string   `json:"status,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	Worktree    string   `json:"worktree,omitempty"`
	Handle      string   `json:"handle,omitempty"`
	Note        string   `json:"note,omitempty"`
	StartedAt   int64    `json:"started_at,omitempty"`
	FinishedAt  int64    `json:"finished_at,omitempty"`
	Live        bool     `json:"live,omitempty"`
	Interrupted bool     `json:"interrupted,omitempty"`
}

// TeamworkJobView 是一行作业投影。Node / Role 是桥给出的**权威归属**（Node = 工作项 id
// 或里程碑 id，Role = 作用域主体 emp_<role> 的反解）；渲染件不再从派发姿势猜归属。
type TeamworkJobView struct {
	Handle   string               `json:"handle"`
	State    string               `json:"state,omitempty"` // running|done|failed|killed（开放取值）
	ExitCode int                  `json:"exit_code,omitempty"`
	Bytes    int64                `json:"bytes,omitempty"`
	Node     string               `json:"node,omitempty"`
	Role     string               `json:"role,omitempty"`
	Scope    TeamworkJobScopeView `json:"scope,omitempty"`
}

// TeamworkJobScopeView 是作业作用域的只读投影（subject = emp_<role>）。
type TeamworkJobScopeView struct {
	Subject string `json:"subject,omitempty"`
}

// TeamworkEventView 是审计流水的一行（只追加、不重写）。
//
// At 用 **unix 秒**：面板上只需要 HH:MM，时间格式化留给消费方（GUI 转 ISO，TUI 本地格式化），
// 投影层不替它们选格式。
type TeamworkEventView struct {
	At        int64  `json:"at,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Role      string `json:"role,omitempty"`
	Handle    string `json:"handle,omitempty"`
	Node      string `json:"node,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	// WorkItem 是 Work Item 口径的归属（甘特节点 id）。
	WorkItem string `json:"work_item,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// TeamworkTeammateMessageView 是尾插进某个 teammate 消息队列的一行（按角色会话号分组）。
type TeamworkTeammateMessageView struct {
	At        int64  `json:"at,omitempty"`
	Role      string `json:"role,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	WorkItem  string `json:"work_item,omitempty"`
	Text      string `json:"text,omitempty"`
}
