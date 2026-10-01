package dto

import "time"

// RoleKind / OrderPolicy / TeamKind 是 A2A 角色工厂的枚举口径。
//
// 长期边界见 docs/arch/a2a-agent-team-factory.md §2：逻辑角色名（role_name）只是
// metadata，provider role 仍只有 system/user/assistant/tool；subagent 是 tool
// calling 能力，不属于任何 RoleKind。
type RoleKind string

const (
	RoleKindUser     RoleKind = "user"
	RoleKindMain     RoleKind = "main"
	RoleKindTechlead RoleKind = "techlead"
	RoleKindAgent    RoleKind = "agent"
	RoleKindTimer    RoleKind = "timer"
)

// OrderPolicy 是 sequencer 的 role 顺序函数口径（唯一可替换点）。
//
// **历史字段（2026-10-01）**：团队顺序的事实正在迁到 team plan 的
// `stages[].depends_on`（leader 掌控，见 docs/arch/teamwork-leader-worker-architecture.md
// §4.6/D4）。现状（可核对）：`order_policy` 落 lifecycle 后只被回读展示
// （dto.TeamView / dto.TeamSchedule），**不驱动轮次**；`order_roles` 仍是座位存在性
// 与发言顺序的事实（goal_coordinator 的 seatPlan）。退场被
// docs/devlog/2026-10-01-m4-deadcode-inventory.md #5 标为 blocked，故这里只标注、
// 不删、**不改落盘取值**（旧会话里的 "goal_loop" 必须继续可读）。
const (
	// Deprecated: 旧环序策略（顺序由这份固定链表给定）。顺序由 leader 编排
	// （team plan），不要按它分支；取值仍要能读旧会话。
	OrderPolicyGoalLoop = "goal_loop"
	// Deprecated: 同 OrderPolicyGoalLoop。
	OrderPolicyUserMainDecided = "user_main_decided"
	// Deprecated: 同 OrderPolicyGoalLoop。
	OrderPolicyScheduledOnly = "scheduled_only"
	DefaultOrderPolicy       = OrderPolicyGoalLoop
)

// DefaultTeamID 是没有团队身份的会话（未装配团队就直接入职）派生角色会话号时用的
// 缺省团队名。
//
// **它不是团队形态**（团队形态目录已于 2026-10-01 删除）：`team_kind` 现在只是
// `team_id` 的展示别名，团队有谁、什么顺序由 TeamSpec/团队库条目说。这个字面量是
// role_session_id 的派生分量——改它 = 既有角色会话号全体分裂，所以保留原值。
//
// 为什么保留原值而不换成中性名：`RoleSessionID(主会话, team_id, role_name)` 是
// 重复装配幂等的键，历史上未装配团队就入职的角色用它算过号；换名会让同一名员工
// 被当成新员工（会话子树、权责反查、项目根绑定全部错位）。要改走一次显式迁移。
const DefaultTeamID = "goal-a2a"

// ToolPolicy 是角色的工具权限口径（RoleSpec.ToolsPolicy 的枚举面）。它同时是
// 员工入职面板里"权限"一栏的取值集合，避免前后端各写一套字符串。
//
// 生效边界（事实，不是承诺）：权限登记的落点是角色注册表（session/team/
// roles.json）；真正的工具拦截在 seelebridge 的 PermissionGate（按会话/全局）。
// 登记值经两条路生效：① 装配期 Runtime.AssignEmployeePermissions 把在编员工落成
// 各自的主体条目 emp_<角色名>；② 角色回合起点（Runtime.RunRoleTurn）按构造把
// 主体放进 ctx。因此"改完注册表"对**下一次员工回合**生效。
//
// ToolsPolicy 只是**档位预设**（readonly/readwrite 两档可分配；空/full = 继承宿主
// 默认）。要给某个员工逐格装配（例如"能写项目但碰不到共享桌面"），用
// RoleSpec.PermissionGroups：显式格子在装配期优先于档位。
const (
	ToolPolicyInherit   = ""         // 空 = 继承宿主默认
	ToolPolicyReadonly  = "readonly" // 只读：不写文件、不执行命令
	ToolPolicyReadWrite = "readwrite"
	ToolPolicyFull      = "full"
)

// ModelPolicy 是角色的模型口径（同见 RoleSpec.ModelPolicy）。空 = 继承宿主模型。
const (
	ModelPolicyInherit    = ""
	ModelPolicySameAsExec = "same-as-exec"
)

// RoleSpec 是角色注册与前端角色管理的最小单位（arch 稿 §2.1）。
// 空字段 = 未配置，继承宿主默认；不表示禁用。
type RoleSpec struct {
	RoleName        string   `json:"role_name"`
	RoleKind        RoleKind `json:"role_kind,omitempty"`
	SystemPrompt    string   `json:"system_prompt,omitempty"`
	ModelPolicy     string   `json:"model_policy,omitempty"`
	MirrorPolicy    []string `json:"mirror_policy,omitempty"`
	DirectiveSchema []string `json:"directive_schema,omitempty"`
	OrderPriority   int      `json:"order_priority,omitempty"`
	JoinPolicy      string   `json:"join_policy,omitempty"`
	PresencePolicy  string   `json:"presence_policy,omitempty"`
	ToolsPolicy     string   `json:"tools_policy,omitempty"`
	// PermissionGroups 是给这个员工**逐格装配**的权限（路由组 → 位）。字段语义
	// 见 NormalizePermissionGroups：空 = 没显式装配（按 ToolsPolicy 档位派生），
	// 非空 = 显式装配（未列出的组 = 0 位）。
	PermissionGroups map[string]uint8 `json:"permission_groups,omitempty"`
}

// TeamSpec 是 AgentTeamFactory 的装配输入（arch 稿 §2.2）。
//
// OrderPolicy / OrderRoles 是**旧的群聊顺序字段**（历史/只读，见 OrderPolicy 常量的
// 说明）：写入面仍在（装配 / 入职 / 团队库保存），读面只用来派座位与展示。新事实 =
// team plan 的 `stages[].depends_on`（leader 掌控）；旧字段退场前不得删。
type TeamSpec struct {
	TeamID        string     `json:"team_id,omitempty"`
	TeamKind      string     `json:"team_kind,omitempty"`
	OrderPolicy   string     `json:"order_policy,omitempty"`
	OrderRoles    []string   `json:"order_roles,omitempty"`
	Roles         []RoleSpec `json:"roles,omitempty"`
	GatePolicy    string     `json:"gate_policy,omitempty"`
	CompactPolicy string     `json:"compact_policy,omitempty"`
}

// TeamRoleSession 是角色注册表里一个角色对应的角色会话坐标。
type TeamRoleSession struct {
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Exists        bool   `json:"exists"`
	Created       bool   `json:"created,omitempty"`
}

// TeamMember 是成员表的只读一行：身份 + 顺序位置 + 是否在 order_roles 内。
// online/floor 高亮是运行态，由 presence 与 message head.floor 提供，不在这里落盘。
//
// SystemPrompt/ModelPolicy/PresencePolicy 是角色配置的回读（前端「编辑员工」
// 面板要回填原值，否则一次编辑就会把提示词/权限清空）。它不是运行时注入的
// 私有指令，而是用户自己登记的员工配置，因此随成员表下发。
type TeamMember struct {
	RoleName       string   `json:"role_name"`
	RoleKind       RoleKind `json:"role_kind,omitempty"`
	RoleSessionID  string   `json:"role_session_id,omitempty"`
	OrderIndex     int      `json:"order_index"` // -1 = 不在工作顺序里（例如定时 agent）
	InOrder        bool     `json:"in_order"`
	OrderPriority  int      `json:"order_priority,omitempty"`
	JoinPolicy     string   `json:"join_policy,omitempty"`
	ToolsPolicy    string   `json:"tools_policy,omitempty"`
	SystemPrompt   string   `json:"system_prompt,omitempty"`
	ModelPolicy    string   `json:"model_policy,omitempty"`
	PresencePolicy string   `json:"presence_policy,omitempty"`
	// PermissionGroups 是这条员工行**逐格装配**的权限回读（前端「编辑员工」面板
	// 要回填原值，否则一次编辑就会把装配好的格子清空）。空 = 没显式装配。
	PermissionGroups map[string]uint8 `json:"permission_groups,omitempty"`
}

// TeamView 是供前端「状态 → Agent Team」子页与角色管理设置消费的装配视图。
type TeamView struct {
	SessionID   string       `json:"session_id"`
	TeamID      string       `json:"team_id,omitempty"`
	TeamKind    string       `json:"team_kind,omitempty"`
	OrderPolicy string       `json:"order_policy,omitempty"`
	OrderRoles  []string     `json:"order_roles,omitempty"`
	Members     []TeamMember `json:"members"`
	Scheduled   []TeamMember `json:"scheduled,omitempty"` // 定时任务 agent 单独分区，不入 order_roles
	Configured  bool         `json:"configured"`
	FloorRole   string       `json:"floor_role,omitempty"`
	// Schedule 是发言调度的只读运行态（链表顺序 + 轮次/无进展记账 + 逃生状态），
	// 由 agentteam.Runtime 投影。缺省 nil = 该会话还没有运行态（前端只显示静态顺序）。
	Schedule     *TeamSchedule `json:"schedule,omitempty"`
	DesignNotice []string      `json:"design_notice,omitempty"`
}

// TeamSchedule 是团队发言调度的只读运行态快照：**谁下一个说**、轮次走到第几轮、
// 逃生路径有没有被触发。事实来源是 agentteam 的链表调度器（顺序仍只有
// lifecycle.order_policy/order_roles 一份，这里只是运行态投影，不落盘）。
//
// OrderPolicy 是**历史字段**（同 OrderPolicy 常量）：这里只是回读展示，不驱动轮次。
type TeamSchedule struct {
	OrderPolicy string `json:"order_policy,omitempty"`
	// Order 是调度器当前维护的**发言顺序成员**（= order_roles − user：user 的发言
	// 机会是回合尾消息队列被整批提升为下一轮，不占顺序里的排班位；见 agentteam.ringOrder）。
	Order []string `json:"order,omitempty"`
	// NextRole 是下一次该发言的角色（空 = 环内没有人可以发言）。
	NextRole string `json:"next_role,omitempty"`
	// Round / RoundLimit 是逃生路径第一道：轮次上限（RoundLimit=0 表示治理层
	// 不设上限，只靠裁决/Break 收束）。
	Round      int `json:"round"`
	RoundLimit int `json:"round_limit"`
	// NoProgress / NoProgressLimit 是逃生路径第二道：连续无进展轮次上限。
	NoProgress      int `json:"no_progress"`
	NoProgressLimit int `json:"no_progress_limit"`
	// Stopped / StopReason 是循环是否已被逃生路径收束（round_limit / no_progress /
	// no_executor / external_break / verdict）。
	Stopped    bool   `json:"stopped"`
	StopReason string `json:"stop_reason,omitempty"`
	// Unexecuted 是环内没有运行时执行者的角色（占位但不会自动产生回合）。
	Unexecuted []string `json:"unexecuted,omitempty"`
	// Prefix 是「team work 起点 → 当前位置」的正文前缀（下一个发言成员拿到的
	// 上下文）。**只读投影**：作者是存储侧对主会话上下文（含主会话 draft）的装配
	// （assembleRoleWire；roleName=main 复用主会话自身，与 TL 对话记录同一条
	// engine loop 口径），投影由 agentteam.Runtime.NoteMainContext 完成。没有任何
	// GUI/端口写入口——前端只能快照查看；若允许前端回写，后端真值会变成前端渲染
	// 结果，前缀随即与帧账本不一致。
	Prefix string `json:"prefix,omitempty"`
	// PrefixParts 是前缀投影出的正文行数（前端显示"已累积 n 行"）。
	PrefixParts int `json:"prefix_parts,omitempty"`
	// PrefixChars 是前缀的字符数（不把整段正文塞进每个快照时的轻量读数）。
	PrefixChars int `json:"prefix_chars,omitempty"`
	// 下面四个是前缀的口径锚点：直接取自装配它的那条 wire，前后端据此核对
	// "看的是同一条 wire"，而不是各自渲染一遍再对不上。
	PrefixDigest      string `json:"prefix_digest,omitempty"`
	PrefixAppliedSeq  uint64 `json:"prefix_applied_seq,omitempty"`
	PrefixTailSeq     uint64 `json:"prefix_tail_seq,omitempty"`
	PrefixNeedCompact bool   `json:"prefix_need_compact,omitempty"`
}

// RoleInstantiation 是「一步实例化一个角色」（员工入职）的可观测回执：配置怎么落、
// 会话建没建、进不进工作顺序、谁在运行时执行它。没有执行者的角色在这里就说清楚，
// 而不是等到 UI 上看起来「有人干活」却没回合。
type RoleInstantiation struct {
	Role        RoleSpec        `json:"role"`
	Session     TeamRoleSession `json:"session"`
	OrderPolicy string          `json:"order_policy,omitempty"`
	OrderRoles  []string        `json:"order_roles,omitempty"`
	InOrder     bool            `json:"in_order"`
	OrderIndex  int             `json:"order_index"` // -1 = 不在工作顺序里
	Executor    string          `json:"executor,omitempty"`
	Notice      []string        `json:"notice,omitempty"`
}

// TeamMaterializeResult 是一次 AgentTeam 装配的可观测结果（headless 巡检面）。
type TeamMaterializeResult struct {
	Spec     TeamSpec          `json:"spec"`
	View     TeamView          `json:"view"`
	Sessions []TeamRoleSession `json:"sessions,omitempty"`
	Registry TeamRegistry      `json:"registry"`
}

// TeamRegistry 是角色注册表的应用层形态（纯 DTO；存储层只在适配器里出现）。
type TeamRegistry struct {
	TeamID      string     `json:"team_id,omitempty"`
	TeamKind    string     `json:"team_kind,omitempty"`
	OrderPolicy string     `json:"order_policy,omitempty"`
	Roles       []RoleSpec `json:"roles,omitempty"`
	Configured  bool       `json:"configured"`
	UpdatedAt   time.Time  `json:"updated_at,omitempty"`
}

// TeamLibraryEntry 是团队库里的一支团队：一支**可复用的团队模板**
// （角色配置集 + 顺序策略），可以装配到任意会话。
//
// 与 TeamRegistry 的区别（别混两份事实）：
//   - TeamRegistry = 某个会话"当前在编的员工表"（per-session 持久事实）；
//   - TeamLibraryEntry = **全局**"团队模板库"（global 作用域，跨项目/跨会话复用）；
//   - 装配动作 = 把库条目 Materialize 成某个会话的 TeamRegistry + 顺序。
type TeamLibraryEntry struct {
	TeamID        string     `json:"team_id"`
	TeamKind      string     `json:"team_kind,omitempty"`
	Name          string     `json:"name,omitempty"`
	OrderPolicy   string     `json:"order_policy,omitempty"`
	OrderRoles    []string   `json:"order_roles,omitempty"`
	Roles         []RoleSpec `json:"roles,omitempty"`
	GatePolicy    string     `json:"gate_policy,omitempty"`
	CompactPolicy string     `json:"compact_policy,omitempty"`
	Origin        string     `json:"origin,omitempty"` // builtin/custom/current-session
	UpdatedAt     time.Time  `json:"updated_at,omitempty"`
}

// TeamLibrary 是团队库的完整内容（整份替换型，与 TeamRegistry 同构；**全局**粒度）。
type TeamLibrary struct {
	Teams      []TeamLibraryEntry `json:"teams"`
	Configured bool               `json:"configured"`
}

// EmployeeLibrary 是全局「员工库」：可复用员工的全局名册（RoleSpec 池）。
// 它是"母本"，会话的在编员工表（TeamRegistry）是它的深拷贝副本。
type EmployeeLibrary struct {
	Employees  []RoleSpec `json:"employees"`
	Configured bool       `json:"configured"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty"`
}

// DefaultOrder 是全局「默认顺序」：没有任何会话/团队显式编排顺序时的默认发言次序。
// 会话内的顺序改动只落会话副本（lifecycle head）；只有「确认普及搭配到全局」才会
// 把会话顺序写回这里。
type DefaultOrder struct {
	OrderPolicy string    `json:"order_policy,omitempty"`
	OrderRoles  []string  `json:"order_roles,omitempty"`
	Configured  bool      `json:"configured"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// TeamComposition 是"某个会话当前的搭配"（会话副本）的只读投影：在编员工 + 顺序。
// 前端拿它与全局母本并列展示；「确认普及搭配到全局」就是把这份投影回写母本。
type TeamComposition struct {
	SessionID   string     `json:"session_id,omitempty"`
	TeamID      string     `json:"team_id,omitempty"`
	TeamKind    string     `json:"team_kind,omitempty"`
	OrderPolicy string     `json:"order_policy,omitempty"`
	OrderRoles  []string   `json:"order_roles,omitempty"`
	Employees   []RoleSpec `json:"employees,omitempty"`
}

// TeamGlobalConfig 是全局母本（团队库 / 员工库 / 默认顺序）的一次读回，附带当前
// 会话副本的搭配投影。
//
// 读语义（用户口径）：返回的是母本的**深拷贝**（消费方拿到的是私有副本，不共享
// 可变切片）；会话读自己的副本（registry + lifecycle head），不持全局写锁。
type TeamGlobalConfig struct {
	Library     TeamLibrary     `json:"library"`
	Employees   EmployeeLibrary `json:"employees"`
	Order       DefaultOrder    `json:"order"`
	Composition TeamComposition `json:"composition"`
}

// RolePromptOptimizeRequest / RolePromptOptimizeResult 是"员工入职"里的
// 提示词优化（一次有界 LLM 回合，不写任何会话消息）。
//
// 边界：优化只产出候选文本；落盘仍走 AgentTeamInstantiateRole/PutRole
// （提示词是角色配置的一部分，用户点"入职"才写）。
type RolePromptOptimizeRequest struct {
	RoleName     string   `json:"role_name,omitempty"`
	RoleKind     string   `json:"role_kind,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	TeamKind     string   `json:"team_kind,omitempty"`
	OrderRoles   []string `json:"order_roles,omitempty"`
	ToolsPolicy  string   `json:"tools_policy,omitempty"`
}

// RolePromptOptimizeResult 回执：优化后的文本 + 为什么这样改 + 用了哪个模型。
type RolePromptOptimizeResult struct {
	RoleName  string   `json:"role_name,omitempty"`
	Original  string   `json:"original,omitempty"`
	Optimized string   `json:"optimized,omitempty"`
	Notes     []string `json:"notes,omitempty"`
	Model     string   `json:"model,omitempty"`
}

// RoleTurnRequest 描述"跑一个角色（员工）的一轮"所需的全部上下文。
//
// 这是 application 侧 RoleTurnRunner 契约的**跨层形态**：application/core 只
// 决定"谁有座位、谁先谁后"，真正的执行体（角色自己的会话 + 工具面 + 权责）
// 由 seelebridge 实现。两个契约字段必须一一对应，否则座位派生的请求到执行体
// 会丢字段（丢 ToolsPolicy = 员工权责无从落地）。
type RoleTurnRequest struct {
	SessionID     string `json:"session_id,omitempty"`      // 主会话（团队注册表/环的属主）
	RoleName      string `json:"role_name,omitempty"`       // 角色名（如 pm / exec / test_case）
	RoleSessionID string `json:"role_session_id,omitempty"` // 角色会话（员工自己的会话）
	ToolsPolicy   string `json:"tools_policy,omitempty"`    // 该角色的权责口径（readonly / readwrite / full）
	OrderIndex    int    `json:"order_index,omitempty"`     // 在发言链里的位次（0 起）
	// PermissionGroups 是该角色**逐格装配**的权限（路由组 → 位）。丢字段等于丢
	// 员工权限：执行体开角色会话时按它分配主体条目，少一份就等于"装配了但没生效"。
	PermissionGroups map[string]uint8 `json:"permission_groups,omitempty"`
	// Input 是本轮该角色拿到的"工作正文"（治理循环的 detail）。为空时执行体
	// 只跑一次"按自己的角色设定继续"的回合，不做任何凭空补全。
	Input string `json:"input,omitempty"`
}

// RoleTurnOutcome 是一轮角色回合的结论：治理循环用它写面板（Note）并喂逃生
// 记账（Progress=false 的轮次会被环的 no_progress 口径计入）。
type RoleTurnOutcome struct {
	Ran      bool   `json:"ran"`      // 真的跑了模型回合（false = 该角色这轮没动）
	Progress bool   `json:"progress"` // 这一轮是否推进了目标
	Note     string `json:"note,omitempty"`
}
