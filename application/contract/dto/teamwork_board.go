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
// （plan.stages / plan.members / plan.milestones / jobs / events）——前端只做搬运，
// 不在搬运里做判定；任何判定（阶段状态折算、拓扑排序、层号）都归渲染件的纯函数。

// TeamworkBoardView 是某会话的团队看板只读投影。
//
// nil 或 Stages 为空 = 该会话没有团队计划：GUI 与 TUI **都不渲染空壳**（口径同目标看板，
// 「结束就是没有了」）。
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
	// Recovered 是**存档兜底**的显式化：活体给不出看板（无计划 / 计划没有阶段）时
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
	Stages       []TeamworkStageView     `json:"stages,omitempty"`
	Members      []TeamworkMemberView    `json:"members,omitempty"`
	Milestones   []TeamworkMilestoneView `json:"milestones,omitempty"`
	Jobs         []TeamworkJobView       `json:"jobs,omitempty"`
	Events       []TeamworkEventView     `json:"events,omitempty"`
}

// TeamworkStageView 是一个编排阶段；DependsOn 是**顺序的唯一事实**
// （与 sessionstore.TeamworkStage 同名搬运，不从派发姿势猜顺序）。
type TeamworkStageView struct {
	ID        string   `json:"id"`
	Roles     []string `json:"roles,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// TeamworkMemberView 是一个在编 teammate。
type TeamworkMemberView struct {
	Role          string `json:"role"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	Worktree      string `json:"worktree,omitempty"`
	ToolsPolicy   string `json:"tools_policy,omitempty"`
}

// TeamworkMilestoneView 是一个里程碑；Status 为空 = pending（与 sessionstore 同口径）。
type TeamworkMilestoneView struct {
	ID      string   `json:"id"`
	After   []string `json:"after,omitempty"`
	Status  string   `json:"status,omitempty"`
	Content string   `json:"content,omitempty"`
}

// TeamworkJobView 是一行作业投影。Stage / Role 是桥给出的**权威归属**：渲染件里那条
// job.stage → job.node → job.scope.subject 的回落链只是过渡口径，接线后 stage 必定命中。
type TeamworkJobView struct {
	Handle   string               `json:"handle"`
	State    string               `json:"state,omitempty"` // running|done|failed|killed（开放取值）
	ExitCode int                  `json:"exit_code,omitempty"`
	Bytes    int64                `json:"bytes,omitempty"`
	Stage    string               `json:"stage,omitempty"`
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
	Stage     string `json:"stage,omitempty"`
	Role      string `json:"role,omitempty"`
	Handle    string `json:"handle,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	Detail    string `json:"detail,omitempty"`
}
