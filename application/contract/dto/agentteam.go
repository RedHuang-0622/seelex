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
	SessionID    string       `json:"session_id"`
	TeamID       string       `json:"team_id,omitempty"`
	TeamKind     string       `json:"team_kind,omitempty"`
	OrderPolicy  string       `json:"order_policy,omitempty"`
	OrderRoles   []string     `json:"order_roles,omitempty"`
	Members      []TeamMember `json:"members"`
	Scheduled    []TeamMember `json:"scheduled,omitempty"` // 定时任务 agent 单独分区，不入 order_roles
	Configured   bool         `json:"configured"`
	FloorRole    string       `json:"floor_role,omitempty"`
	DesignNotice []string     `json:"design_notice,omitempty"`
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
