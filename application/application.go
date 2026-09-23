// Package application is the stable facade for the Seelex application core.
//
// Implementation is grouped into focused subpackages; consumers should keep
// importing this package so their contracts remain stable as internals evolve.
package application

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/seelebridge/search"
)

type (
	Service                    = core.Service
	ToolHookBridge             = core.ToolHookBridge
	ToolHookDiagnosticEvent    = core.ToolHookDiagnosticEvent
	ToolHookDiagnosticObserver = core.ToolHookDiagnosticObserver
	Suggestion                 = core.Suggestion
	Command                    = core.Command
	CommandResult              = core.CommandResult
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
	SessionInfo                = model.SessionInfo
	SessionMeta                = model.SessionMeta
	WorkspaceInfo              = model.WorkspaceInfo
	Interaction                = model.Interaction
	InteractionOption          = model.InteractionOption
	Capabilities               = model.Capabilities
	EventKind                  = event.EventKind
	Event                      = event.Event
	MessageDelta               = event.MessageDelta
	CompactionProgress         = event.CompactionProgress
	ReplayResult               = event.ReplayResult
	Subscription               = event.Subscription
	EventHub                   = event.EventHub
	ApprovalRequest            = approval.ApprovalRequest
	ApprovalDecision           = approval.ApprovalDecision
	ApprovalBroker             = approval.ApprovalBroker
	PromptLayer                = prompt.PromptLayer
	PromptStack                = prompt.PromptStack
	EffortManager              = prompt.EffortManager
	WebSearchConfig            = search.WebSearchConfig
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
	EventInteractionOpened     = event.EventInteractionOpened
	EventInteractionClosed     = event.EventInteractionClosed
	EventError                 = event.EventError
	EventResyncRequired        = event.EventResyncRequired
	EventExitRequested         = event.EventExitRequested
	EventViewSessionChanged    = event.EventViewSessionChanged
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
)

var (
	ErrChatRunning         = core.ErrChatRunning
	ErrApplicationDraining = core.ErrApplicationDraining
	ErrInteractionNotFound = approval.ErrInteractionNotFound
	ErrInteractionResolved = approval.ErrInteractionResolved
)

// ── 输入前缀（sigil）契约 ────────────────────────────────────────────────
//
// 前缀与含义一一对应：`/` 可执行入口（命令 + Skill；工具不列出——只有模型能调用，
// 要用户显式调用先注册成命令）、`#` 切换插件、`$` 召回 Skill、`@` 手动召唤团队。
// 权威说明见 docs/gui/modules/shell-and-interactions.md；
// 前端（含 TUI）只消费这里的常量与判定，不自持第二份字符表。
const (
	SigilCommand = core.SigilCommand
	SigilPlugin  = core.SigilPlugin
	SigilSkill   = core.SigilSkill
	SigilTeam    = core.SigilTeam
)

// HasSigilPrefix 报告输入是否以前缀开头（TUI suggMode / 前端内联建议共用）。
func HasSigilPrefix(input string) bool { return core.HasSigilPrefix(input) }

// SigilOf 返回输入的首字符前缀；不可解析时返回空串。
func SigilOf(input string) string { return core.SigilOf(input) }

func New(deps Dependencies) (*Service, error) { return core.New(deps) }

func NewEventHub() *EventHub { return event.NewEventHub() }

func NewApprovalBroker(events *EventHub) *ApprovalBroker { return approval.NewApprovalBroker(events) }

func NewToolHookBridge() *ToolHookBridge { return core.NewToolHookBridge() }

func NewPromptStack() *PromptStack { return prompt.NewPromptStack() }

func NewEffortManager(stack *PromptStack, engine interface {
	SetMaxLoops(int)
	SetSystemPrompt(string)
}) *EffortManager {
	return prompt.NewEffortManager(stack, engine)
}

func ValidEffortLevels() []string { return prompt.ValidEffortLevels() }

func RenderDiag(snapshot Snapshot) string { return core.RenderDiag(snapshot) }

func WebSearch(ctx context.Context, config WebSearchConfig, query string, maxResults int) (string, error) {
	return search.WebSearch(ctx, config, query, maxResults)
}
