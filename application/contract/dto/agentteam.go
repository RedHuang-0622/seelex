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
// **历史字段，只读兼容（read-compat）**：团队顺序的事实已经迁到 team plan 里
// （`milestones[].depends_on` 屏障 + `milestones[].items[].depends_on` 里程碑内 DAG，
// leader 掌控，见 docs/arch/teamwork-leader-worker-architecture.md §4.6/D4）。
//
// 现状（2026-10-15，A05 复核）：
//   - `order_policy` / `order_roles` 落 lifecycle 后只被回读展示，**不驱动任何行为**：
//     seatPlan / 座位轮转（2026-10-03）与发言环（`agentteam.Runtime`，A04 2026-10-15）
//     都已删除，没有任何运行时消费者；
//   - 剩下的写入口只有 headless `role.set_order` 与 `internal/adapters` 的端口适配
//     （见 gui/headless.go），产品 UI 已无装配/调序入口；
//   - **不删、不改落盘取值**：旧会话里的 "goal_loop" 等取值必须继续可读；字段清除
//     属于独立的版本化迁移（旧会话样例 + 冷恢复路径 + 回滚方案齐备后才做），
//     不与"删退场面"同批。历史决策见
//     docs/devlog/2026-10-01-m4-deadcode-inventory.md #5 与 A05 条目。
//
// （team plan 里的 `stages` 是另一回事：那个形状已整条退场。）
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
// 各自的主体条目 emp_<角色名>；② 角色回合起点（Runtime.runRoleRound，由 leader 派发的
// worker 作业与 ADVISOR 评审共用）按构造把主体放进 ctx。因此"改完注册表"对
// **下一次员工回合**生效。
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
//
// 状态（A06/A07，2026-10-15）：**不是 legacy 整体**。它的登记来源（员工库 / 入职面板 /
// 会话注册表）已随 J-1a 退场，但 `ToolsPolicy` / `PermissionGroups` / `Plugins` 仍是
// 队友执行链的活动配置——角色回合（`seelebridge` 的 `runRoleRound`）按它装配工具面与
// 插件，权限门与审批归属是安全边界。删这些字段等于放宽权限或让插件装配落空。
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
	// Plugins 是按会话插件装配（能力轴；见
	// docs/devlog/2026-10-05-teammate-plugin-assembly-design.md）。空/缺失 = 不覆盖：
	// 工具面继承宿主当前装配、技能目录不注入。
	//
	// 硬规则：**权限不随插件走**——插件只带能力（技能 / 提示词 / 工具收窄），
	// tools_policy / permission_groups / MCP / hooks 一律不得由插件供货。工具面
	// = 权限面 ∩ 插件面，插件只收窄、永不放宽。
	//
	// 上限 MaxPluginsPerRole 个（出厂 3；配置键 limits.plugins.per_teammate 可调）。
	Plugins []string `json:"plugins,omitempty"`
}

// TeamSpec 是 AgentTeamFactory 的装配输入（arch 稿 §2.2）。
//
// OrderPolicy / OrderRoles 是**旧的群聊顺序字段**（历史/只读兼容，见 OrderPolicy 常量的
// 说明）：装配 / 入职 / 团队库保存这些写入面已随 J-1a 退场，现在只剩 headless
// `role.set_order` 与适配器端口；读面只用来展示顺序，不驱动行为。新事实 = team plan 的
// 里程碑屏障 `milestones[].depends_on` 与里程碑内 DAG `items[].depends_on`（leader 掌控）；
// 旧字段退场前不得删（需要独立的版本化迁移）。
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
//
// legacy（A06，2026-10-15）：注册表装配面已随 J-1a 退场，这里只留形状；角色会话本身
// 仍由 `role.*` / worker 作业面在跑，不要把这个"坐标 DTO"当成退场能力一起删。
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
	// 这个员工行是折叠的只读回读：Plugins（按会话插件装配）也是回读面之一——前端
	// 要回填原值，否则一次编辑就会把装配清空（与 SystemPrompt/ToolsPolicy 同一
	// 理由）。空 = 不覆盖（继承宿主当前装配 + 技能目录不注入）。
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
	// Plugins 是按会话插件装配（能力轴；见
	// docs/devlog/2026-10-05-teammate-plugin-assembly-design.md）。空/缺失 = 不覆盖：
	// 工具面继承宿主当前装配、技能目录不注入。
	//
	// 硬规则：**权限不随插件走**——插件只带能力（技能 / 提示词 / 工具收窄），
	// tools_policy / permission_groups / MCP / hooks 一律不得由插件供货。工具面
	// = 权限面 ∩ 插件面，插件只收窄、永不放宽。
	//
	// 上限 MaxPluginsPerRole 个（出厂 3；配置键 limits.plugins.per_teammate 可调）。
	Plugins []string `json:"plugins,omitempty"`
}

// TeamView 是供前端「状态 → Agent Team」子页与角色管理设置消费的装配视图。
//
// legacy（A06，2026-10-15）：装配面（`AgentTeamView` / `assembleView` / 面板）已随
// J-1a 退场，本类型**没有生产者**——`application/core` 的
// `Service.sessionAgentTeamViewJ1A` 恒返回零值（见 A13）。保留是为了旧 wire 形状与
// 只读兼容；成员表/floor/DesignNotice 都不再由产品写入。
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
	// Schedule 是发言调度的只读运行态（链表顺序 + 轮次/无进展记账 + 逃生状态）。
	//
	// **历史 wire，只读兼容（A05，2026-10-15）**：唯一的投影作者 `agentteam.Runtime`
	// 已随发言环在 A04 整条删除，因此这个字段现在**没有任何生产者**（恒 nil），
	// 保留只是为了旧快照/夹具仍能解码、以及不打断 headless 巡检的 JSON 形状。
	// 移除它必须与 GUI/headless 的 API 版本一起做（见 TeamSchedule 的说明）。
	Schedule     *TeamSchedule `json:"schedule,omitempty"`
	DesignNotice []string      `json:"design_notice,omitempty"`
}

// TeamSchedule 是团队发言调度的只读运行态快照：**谁下一个说**、轮次走到第几轮、
// 逃生路径有没有被触发。事实来源曾是 agentteam 的链表调度器（顺序仍只有
// lifecycle.order_policy/order_roles 一份，它只是运行态投影，不落盘）。
//
// **历史 wire，只读兼容（A05，2026-10-15）**
//
// 生产者（`agentteam.Runtime` / `TurnScheduler`：环成员、逃生记账、前缀投影）已在
// A04 随发言环整条删除，且不再有替代实现——"下一个谁发言"不是产品能力，让角色说话的是
// leader 的 `team_dispatch`。因此：
//   - 本类型**没有生产者**，字段全部只读；任何新代码不得再按它推导行为；
//   - 保留是为了旧快照/夹具可解码 + 不打断 headless 巡检的 JSON 形状；
//   - **兼容期限**：与 `dto.TeamView.Schedule` 一起，到下一次 GUI/headless API
//     版本变更时随该版本一并移除（届时需要：旧载荷 fixture 冷读用例 + 前后端同批改动）。
//     在此之前不得单独删字段（旧数据/旧载荷要能读回来）。
//
// OrderPolicy 是**历史字段**（同 OrderPolicy 常量）：这里只是回读展示，不驱动轮次。
type TeamSchedule struct {
	OrderPolicy string `json:"order_policy,omitempty"`
	// Order 是调度器当前维护的**发言顺序成员**（= order_roles − user：user 的发言
	// 机会是回合尾消息队列被整批提升为下一轮，不占顺序里的排班位；那条投影
	// `agentteam.ringOrder` 随环在 A04 删除，这里只留历史语义）。
	Order []string `json:"order,omitempty"`
	// NextRole 是历史上"下一次该发言的角色"（空 = 环内没有人可以发言）。环已删除，
	// 该读数不再由任何代码产生。
	NextRole string `json:"next_role,omitempty"`
	// Round / RoundLimit 是逃生路径第一道：环自己的轮次上限（RoundLimit=0 表示
	// 环不设上限，只靠显式收束）。2026-10-03 起它与 goal 治理**不同源**：
	// goal 没有"轮次"了，这是环自己的兜底（`defaultTeamRoundLimit` 随环在 A04 删除）。
	Round      int `json:"round"`
	RoundLimit int `json:"round_limit"`
	// NoProgress / NoProgressLimit 是逃生路径第二道：连续无进展轮次上限。
	NoProgress      int `json:"no_progress"`
	NoProgressLimit int `json:"no_progress_limit"`
	// Stopped / StopReason 是循环是否已被逃生路径收束（round_limit / no_progress /
	// no_automatic_turn / external_break / verdict）。
	Stopped    bool   `json:"stopped"`
	StopReason string `json:"stop_reason,omitempty"`
	// NoAutomaticTurn 是环内没有自动回合的角色（占位、不会自己发言；leader 派活才跑）。
	// 事实表 `agentteam.AutomaticTurnRoles` 随环在 A04 删除。
	NoAutomaticTurn []string `json:"no_automatic_turn,omitempty"`
	// Prefix 是「team work 起点 → 当前位置」的正文前缀（下一个发言成员拿到的
	// 上下文）。**只读投影**：作者是存储侧对主会话上下文（含主会话 draft）的装配
	// （assembleRoleWire；roleName=main 复用主会话自身，与 TL 对话记录同一条
	// engine loop 口径），投影曾是 agentteam.Runtime.NoteMainContext（随环在 A04 删除，
	// 此前 J-1a/A03 已删掉它的应用层作者）。没有任何 GUI/端口写入口——前端只能快照查看；
	// 若允许前端回写，后端真值会变成前端渲染结果，前缀随即与帧账本不一致。
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

// ── A06：旧 Agent Team 注册表 / 团队库 / 员工库 / 母本 —— legacy 只读兼容 ──────
//
// 下面这一组类型（RoleInstantiation / TeamMaterializeResult / TeamRegistry /
// TeamLibrary* / EmployeeLibrary / DefaultOrder / TeamComposition / TeamGlobalConfig）是
// Agent Team 装配面的 wire 形状。用户面（装配 / 入职 / 团队库 / 员工库 / 全局母本）已于
// 2026-10-12 J-1a 退场；2026-10-15 A06 复核时**没有找到任何生产应用适配器调用**
// （静态全仓索引只剩 dto 自身、测试夹具与 sessionstore 的读写实现）。
//
// 兼容政策（A06；与"删退场面"分开执行）：
//  1. **保留读能力**：`sessionstore/team_registry.go` 的 `ReadTeamRegistryWorkspace` /
//     `WriteTeamRegistryWorkspace` / `RemoveTeamRegistryWorkspace` 一字未改，旧会话里的
//     `session/team/roles.json` 仍可读出、诊断与迁移——删 reader 会让旧数据失去唯一入口，
//     那不是清理；
//  2. **不删文件、不迁移、不停止写入**：旧数据的清理/迁移必须另行中文预警 + 备份 + 用户
//     确认（MEMORY.md 与 AGENTS.md §0），本条目不做任何磁盘动作；
//  3. 将来若确认产品不再读这些形状，也只先停止写入；DTO/端口的移除必须按**版本化 API
//     变更**做（含旧 roles.json fixture 的冷读用例）；
//  4. 例外（**不是** legacy，不许跟着删）：`RoleSpec.ToolsPolicy` / `PermissionGroups` /
//     `Plugins` 与 `RolePromptOptimize*` 背后的端口仍是活动执行/能力面的配置（A07/A08）。

// RoleInstantiation 是「一步实例化一个角色」（员工入职）的可观测回执：配置怎么落、
// 会话建没建、进不进工作顺序、有没有自动回合（谁会自己发言）。没有自动回合的角色
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
//
// legacy（A06）：装配入口已退场，无生产者。
type TeamMaterializeResult struct {
	Spec     TeamSpec          `json:"spec"`
	View     TeamView          `json:"view"`
	Sessions []TeamRoleSession `json:"sessions,omitempty"`
	Registry TeamRegistry      `json:"registry"`
}

// TeamRegistry 是角色注册表的应用层形态（纯 DTO；存储层只在适配器里出现）。
//
// legacy（A06）：读写实现保留在 `sessionstore/team_registry.go`（`session/team/roles.json`），
// 当前没有生产应用适配器调用它——它现在是**旧数据兼容与诊断面**，不是活动装配面。
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
// legacy（A06）：团队库（全局 `<root>/team/library.json`）与其 CRUD 已随 J-1a 退场，无生产者。
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
//
// legacy（A06）：同 TeamLibraryEntry。
type TeamLibrary struct {
	Teams      []TeamLibraryEntry `json:"teams"`
	Configured bool               `json:"configured"`
}

// EmployeeLibrary 是全局「员工库」：可复用员工的全局名册（RoleSpec 池）。
//
// legacy（A06）：员工库（全局 "母本"）读写面已随 J-1a 退场，无生产者；Test-only 夹具
// （`application/core/agentteam_fixture_test.go` 的 librarySessions）仍实现它的端口形状。
// 它是"母本"，会话的在编员工表（TeamRegistry）是它的深拷贝副本。
type EmployeeLibrary struct {
	Employees  []RoleSpec `json:"employees"`
	Configured bool       `json:"configured"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty"`
}

// DefaultOrder 是全局「默认顺序」：没有任何会话/团队显式编排顺序时的默认发言次序。
//
// legacy（A06）：同 EmployeeLibrary（全局母本已退场，无生产者）。
// 会话内的顺序改动只落会话副本（lifecycle head）；只有「确认普及搭配到全局」才会
// 把会话顺序写回这里。
type DefaultOrder struct {
	OrderPolicy string    `json:"order_policy,omitempty"`
	OrderRoles  []string  `json:"order_roles,omitempty"`
	Configured  bool      `json:"configured"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// TeamComposition 是"某个会话当前的搭配"（会话副本）的只读投影：在编员工 + 顺序。
//
// legacy（A06）：随装配/母本面退场，无生产者。
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
// legacy（A06）：随母本面退场，无生产者，也无消费者（静态索引只剩本文件）。
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
// 边界：优化只产出候选文本；落盘曾走 AgentTeamInstantiateRole/PutRole
// （提示词是角色配置的一部分，用户点"入职"才写）。
//
// 状态（A08，2026-10-15 待裁决）：**端口与实现仍在**（`contract.RolePromptPort`、
// `seelebridge/runtime_role_prompt.go` 的 `OptimizeRolePrompt`），但入口（员工入职面板）
// 已随 J-1a 退场，静态索引找不到应用层生产调用者。本条目只标注现状，不删——
// 它可能是宿主/第三方 API 的对外契约（另见 A08 的处置口径）。
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
