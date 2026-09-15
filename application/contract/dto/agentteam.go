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
const (
	OrderPolicyGoalLoop        = "goal_loop"
	OrderPolicyUserMainDecided = "user_main_decided"
	OrderPolicyScheduledOnly   = "scheduled_only"
	DefaultOrderPolicy         = OrderPolicyGoalLoop
	DefaultTeamKind            = TeamKindGoalA2A
	TeamKindGoalA2A            = "goal-a2a"
	TeamKindReview             = "review-team"
	TeamKindResearch           = "research-team"
)

// RoleSpec 是角色注册与前端角色管理的最小单位（arch 稿 §2.1）。
// 空字段 = 未配置，继承 preset 默认；不表示禁用。
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
}

// TeamSpec 是 AgentTeamFactory 的装配输入（arch 稿 §2.2）。
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
type TeamMember struct {
	RoleName      string   `json:"role_name"`
	RoleKind      RoleKind `json:"role_kind,omitempty"`
	RoleSessionID string   `json:"role_session_id,omitempty"`
	OrderIndex    int      `json:"order_index"` // -1 = 不在工作顺序里（例如定时 agent）
	InOrder       bool     `json:"in_order"`
	OrderPriority int      `json:"order_priority,omitempty"`
	JoinPolicy    string   `json:"join_policy,omitempty"`
	ToolsPolicy   string   `json:"tools_policy,omitempty"`
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

// TeamSchedule 是团队发言调度的只读运行态快照：**谁下一个说**、循环走到第几轮、
// 逃生路径有没有被触发。事实来源是 agentteam 的链表调度器（顺序仍只有
// lifecycle.order_policy/order_roles 一份，这里只是运行态投影，不落盘）。
type TeamSchedule struct {
	OrderPolicy string `json:"order_policy,omitempty"`
	// Order 是调度器当前维护的链表顺序（与 TeamView.OrderRoles 同源，但反映
	// 运行态对齐后的实际次序）。
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
	// UserSeat 是 user 是否作为循环里的一环：queued（默认，仅当 user 有排队输入
	// 才占位）/ member（与员工同权，每轮固定占位）/ absent（不占位）。
	UserSeat string `json:"user_seat,omitempty"`
	// Unexecuted 是环内没有运行时执行者的角色（占位但不会自动产生回合）。
	Unexecuted []string `json:"unexecuted,omitempty"`
}

// User seat 口径：user 可以通过消息队列插入会话（queued），也可以与员工同权
// 固定占位（member），或完全不参与 agent 循环（absent）。
const (
	UserSeatQueued = "queued"
	UserSeatMember = "member"
	UserSeatAbsent = "absent"
)

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
