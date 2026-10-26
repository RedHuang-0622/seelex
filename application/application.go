// Package application is the stable facade for the Seelex application core.
//
// Implementation is grouped into focused subpackages; consumers should keep
// importing this package so their contracts remain stable as internals evolve.
package application

import (
	applicationcompat "github.com/RedHuang-0622/seelex/application/compat"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type (
	Service                    = core.Service
	Frontend                   = applicationcompat.Frontend
	ToolHookBridge             = core.ToolHookBridge
	ToolHookDiagnosticEvent    = core.ToolHookDiagnosticEvent
	ToolHookDiagnosticObserver = core.ToolHookDiagnosticObserver
	Suggestion                 = core.Suggestion
	Command                    = contract.Command
	CommandResult              = contract.CommandResult
	Dependencies               = contract.Dependencies
	EngineMessage              = contract.EngineMessage
	EngineToolCall             = contract.EngineToolCall
	ChatEngine                 = contract.ChatEngine
	RuntimePort                = contract.RuntimePort
	PluginPort                 = contract.PluginPort
	SkillPort                  = contract.SkillPort
	SessionPort                = contract.SessionPort
	WorkspacePort              = contract.WorkspacePort
	Snapshot                   = model.Snapshot
	SessionSnapshot            = model.SessionSnapshot
	ProcessSnapshot            = model.ProcessSnapshot
	SessionRuntime             = model.SessionRuntime
	ProcessRuntime             = model.ProcessRuntime
	SessionState               = model.SessionState
	SessionStatus              = model.SessionStatus
	Message                    = model.Message
	ToolCall                   = model.ToolCall
	ChatState                  = model.ChatState
	TaskState                  = model.TaskState
	ContextCompaction          = model.ContextCompaction
	ReadFileRef                = model.ReadFileRef
	SessionTitle               = model.SessionTitle
	ConversationRecord         = model.ConversationRecord
	SessionPlanFrame           = model.SessionPlanFrame
	SessionExecutionRecord     = model.SessionExecutionRecord
	SessionRecord              = model.SessionRecord
	SessionArchive             = model.SessionArchive
	ActiveSkill                = model.ActiveSkill
	ActivePlanProjection       = model.ActivePlanProjection
	EventRange                 = model.EventRange
	TaskCheckpoint             = model.TaskCheckpoint
	TaskContextProjection      = model.TaskContextProjection
	TokenAudit                 = model.TokenAudit
	TranscriptEvent            = model.TranscriptEvent
	TranscriptToolCall         = model.TranscriptToolCall
	ToolResultRef              = model.ToolResultRef
	StoredToolResult           = model.StoredToolResult
	ToolResultPage             = model.ToolResultPage
	PerfStats                  = model.PerfStats
	ToolRecordBoard            = model.ToolRecordBoard
	ToolRecordStat             = model.ToolRecordStat
	ToolRecordReason           = model.ToolRecordReason
	ToolRecordSnapshot         = model.ToolRecordSnapshot
	RuntimeState               = model.RuntimeState
	ReplanMonitor              = model.ReplanMonitor
	PlanState                  = model.PlanState
	PlanStatus                 = model.PlanStatus
	PlanNode                   = model.PlanNode
	NodeStatus                 = model.NodeStatus
	SubagentDetail             = model.SubagentDetail
	SubagentEvent              = model.SubagentEvent
	SubagentToolEvent          = model.SubagentToolEvent
	Tool                       = model.Tool
	SkillInfo                  = model.SkillInfo
	PluginInfo                 = model.PluginInfo
	AccountInfo                = model.AccountInfo
	// 账号的两条界面链路（添加 / 查余额）：dto 是跨层纯数据，别名与上面几个同一条
	// 口径——前端只借类型身份，不直接引 contract/dto。
	AccountDraft     = dto.AccountDraft
	AccountAddResult = dto.AccountAddResult
	AccountDetails   = dto.AccountDetails
	AccountUpdate    = dto.AccountUpdate
	AccountBalance   = dto.AccountBalance
	// AccountBalanceInfo 是余额读数里的一种币种（界面对账要按币种显示）。
	AccountBalanceInfo = dto.AccountBalanceInfo
	SessionInfo        = model.SessionInfo
	SessionMeta        = model.SessionMeta
	WorkspaceInfo      = model.WorkspaceInfo
	Interaction        = model.Interaction
	InteractionOption  = model.InteractionOption
	Capabilities       = model.Capabilities
	EventKind          = event.EventKind
	Event              = event.Event
	MessageDelta       = event.MessageDelta
	CompactionProgress = event.CompactionProgress
	ReplayResult       = event.ReplayResult
	Subscription       = event.Subscription
	EventHub           = event.EventHub
	// 审批端口形状的事实源在 contract（见 contract/approval.go）；实现体在
	// backend/limit/approval。门面这里只是把两者按既有名字再导出。
	ApprovalRequest  = contract.ApprovalRequest
	ApprovalDecision = contract.ApprovalDecision
	ApprovalBroker   = contract.ApprovalBroker
	PromptLayer      = prompt.PromptLayer
	PromptStack      = prompt.PromptStack
	EffortManager    = prompt.EffortManager
	WebSearchConfig  = dto.WebSearchConfig
	// 会话历史检索结果（`SearchHistory` 用例的返回形状，GUI 历史检索面板
	// 数据源）。终局（规划）：plan §2.1 的 `contract.SearchPort`（M4）。
	SearchResult = dto.SearchResult
)

const (
	ProtocolVersion            = model.ProtocolVersion
	EventSnapshotChanged       = event.EventSnapshotChanged
	EventMessageAdded          = event.EventMessageAdded
	EventMessageDelta          = event.EventMessageDelta
	EventToolStarted           = event.EventToolStarted
	EventToolCompleted         = event.EventToolCompleted
	EventSubagentChanged       = event.EventSubagentChanged
	EventSubagentToolStarted   = event.EventSubagentToolStarted
	EventSubagentToolCompleted = event.EventSubagentToolCompleted
	EventRuntimeChanged        = event.EventRuntimeChanged
	EventTeamChanged           = event.EventTeamChanged
	EventTeammateToolStarted   = event.EventTeammateToolStarted
	EventTeammateToolCompleted = event.EventTeammateToolCompleted
	EventInteractionOpened     = event.EventInteractionOpened
	EventInteractionClosed     = event.EventInteractionClosed
	EventError                 = event.EventError
	EventResyncRequired        = event.EventResyncRequired
	EventExitRequested         = event.EventExitRequested
	EventViewSessionChanged    = event.EventViewSessionChanged
	EventSessionsChanged       = event.EventSessionsChanged
	// 上下文压缩的门禁进度：宿主按 kind 分流才能渲染它，因此事件名与载荷类型都
	// 在门面上（与 MessageDelta 同理——前端/控制台都只消费这一份常量）。
	EventCompactionProgress   = event.EventCompactionProgress
	CompactionProgressRunning = event.CompactionProgressRunning
	CompactionProgressDone    = event.CompactionProgressDone
	CompactionProgressFailed  = event.CompactionProgressFailed
	CompactionPhaseBegin      = event.CompactionPhaseBegin
	PlanPending               = model.PlanPending
	PlanRunning               = model.PlanRunning
	PlanCompleted             = model.PlanCompleted
	PlanFailed                = model.PlanFailed
	PlanAborted               = model.PlanAborted
	NodePending               = model.NodePending
	NodeQueued                = model.NodeQueued
	NodeRunning               = model.NodeRunning
	NodeWorktreeCreating      = model.NodeWorktreeCreating
	NodeRebasing              = model.NodeRebasing
	NodeMerging               = model.NodeMerging
	NodeCompleted             = model.NodeCompleted
	NodeFailed                = model.NodeFailed
	NodeAborted               = model.NodeAborted
	NodeSkipped               = model.NodeSkipped
	NodeCanceled              = model.NodeCanceled
	NodePanicked              = model.NodePanicked
	// 会话可见状态：前端（GUI/TUI）按这些词渲染目录行徽标，因此门面上必须看得见
	// ——guest 端不许再写一份字面量（tui 曾经有过一份 `awaiting_approval`）。
	SessionStatusUnknown          = model.SessionStatusUnknown
	SessionStatusDraft            = model.SessionStatusDraft
	SessionStatusIdle             = model.SessionStatusIdle
	SessionStatusRunning          = model.SessionStatusRunning
	SessionStatusQueued           = model.SessionStatusQueued
	SessionStatusAwaitingApproval = model.SessionStatusAwaitingApproval
	SessionStatusArchived         = model.SessionStatusArchived
	SessionStatusRestoring        = model.SessionStatusRestoring
)

var (
	ErrChatRunning         = core.ErrChatRunning
	ErrApplicationDraining = core.ErrApplicationDraining
	ErrInteractionNotFound = contract.ErrInteractionNotFound
	ErrInteractionResolved = contract.ErrInteractionResolved
)

// ── 输入前缀（sigil）契约 ────────────────────────────────────────────────
//
// 前缀与含义一一对应：`/` 可执行入口（命令 + Skill；工具不列出——只有模型能调用，
// 要用户显式调用先注册成命令）、`#` 切换插件、`$` 召回 Skill。
//
// `@` 手动召唤团队**已退场**（2026-10-12 J-1a）：这个字符让给文件指代（`@文件`），
// Agent Team 的整条用户面（召唤 / 团队库 / 员工库 / 母本）随之退场，不再有
// SigilTeam 这个常量。权威说明见 docs/gui/modules/shell-and-interactions.md；
// 前端（含 TUI）只消费这里的常量与判定，不自持第二份字符表。
const (
	SigilCommand = core.SigilCommand
	SigilPlugin  = core.SigilPlugin
	SigilSkill   = core.SigilSkill
)

// HasSigilPrefix 报告输入是否以前缀开头（TUI suggMode / 前端内联建议共用）。
func HasSigilPrefix(input string) bool { return core.HasSigilPrefix(input) }

// SigilOf 返回输入的首字符前缀；不可解析时返回空串。
func SigilOf(input string) string { return core.SigilOf(input) }

// ValidEffortLevels 返回合法的推理档位名（门面 E 组：无状态纯函数）。
func ValidEffortLevels() []string { return prompt.ValidEffortLevels() }

// AccountWindowPresets 返回"每账号上下文窗口"的界面挡位（token，升序）。
//
// 挡位表只有一处（`seelexctx` 的 limits 段），前端不自己写死一份——与权限档位、
// effort 档位同一条口径（门面 E 组：无状态纯函数）。
func AccountWindowPresets() []int { return dto.AccountWindowPresets() }

// AccountRoles 返回可手动添加的账号角色名（界面「设置 → 账户 → 添加」的角色下拉）。
//
// 与 AccountWindowPresets 同一条口径：**词表只有一处**（contract/dto 的 AccountRole
// 常量），前端从宿主读，不自己写死一份——加角色时不会出现"后端认、界面不认"。
func AccountRoles() []string { return dto.AccountRoleVocabulary() }

// RenderDiag 构建诊断文本，供 `/diag` 命令使用（门面 E 组：无状态纯函数）。
func RenderDiag(snapshot Snapshot) string { return core.RenderDiag(snapshot) }

// ── 门面不持有构造函数（M1-B） ──────────────────────────────────────────
//
// 构造只发生在组合根：`New` / `NewEventHub` / `NewApprovalBroker` /
// `NewToolHookBridge` / `NewPromptStack` / `NewEffortManager` / `WebSearch` 已按
// docs/2026-10-15-architecture-reshape/plan.md §2.2 迁到 `wiring/`（WebSearch 的
// 终局是 `backend/tool/websearch`，见 M4）。前端只消费这里的类型身份、常量与
// 用例方法面；组合根与测试基座从 `wiring` 取构造函数。
//
// 门面"包内无 func New*"是可机械断言的不变量（I3，见 M1-D）。
