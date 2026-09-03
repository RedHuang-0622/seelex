package core

import (
	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/contract"
	cc "github.com/RedHuang-0622/seelex/application/core/context_control"
	"github.com/RedHuang-0622/seelex/application/core/input_router"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
)

type (
	Dependencies           = contract.Dependencies
	Command                = input_router.Command
	CommandResult          = input_router.CommandResult
	CommandRegistry        = input_router.CommandRegistry
	EngineMessage          = contract.EngineMessage
	EngineToolCall         = contract.EngineToolCall
	ChatEngine             = contract.ChatEngine
	RuntimePort            = contract.RuntimePort
	PluginPort             = contract.PluginPort
	SkillPort              = contract.SkillPort
	SessionPort            = contract.SessionPort
	WorkspacePort          = contract.WorkspacePort
	WorkspaceInfo          = model.WorkspaceInfo
	Snapshot               = model.Snapshot
	SessionSnapshot        = model.SessionSnapshot
	ProcessSnapshot        = model.ProcessSnapshot
	SessionRuntime         = model.SessionRuntime
	ProcessRuntime         = model.ProcessRuntime
	SessionState           = model.SessionState
	SessionStatus          = model.SessionStatus
	Message                = model.Message
	ToolCall               = model.ToolCall
	ChatState              = model.ChatState
	TaskState              = model.TaskState
	TaskStatus             = model.TaskStatus
	ContextCompaction      = model.ContextCompaction
	ReadFileRef            = model.ReadFileRef
	SessionTitle           = model.SessionTitle
	ConversationRecord     = model.ConversationRecord
	SessionPlanFrame       = model.SessionPlanFrame
	SessionExecutionRecord = model.SessionExecutionRecord
	SessionRecord          = model.SessionRecord
	ForkRequest            = model.ForkRequest
	SessionForkRef         = model.SessionForkRef
	ForkPoint              = model.ForkPoint
	SessionArchive         = model.SessionArchive
	ActiveSkill            = model.ActiveSkill
	ActivePlanProjection   = model.ActivePlanProjection
	EventRange             = model.EventRange
	TaskCheckpoint         = model.TaskCheckpoint
	TaskContextProjection  = model.TaskContextProjection
	TokenAudit             = model.TokenAudit
	TranscriptEvent        = model.TranscriptEvent
	TranscriptToolCall     = model.TranscriptToolCall
	ToolResultRef          = model.ToolResultRef
	StoredToolResult       = model.StoredToolResult
	RuntimeState           = model.RuntimeState
	ReplanMonitor          = model.ReplanMonitor
	PlanState              = model.PlanState
	PlanStatus             = model.PlanStatus
	PlanNode               = model.PlanNode
	PlanNodeEventInfo      = model.PlanNodeEventInfo
	SubagentEvent          = model.SubagentEvent
	SubagentToolEvent      = model.SubagentToolEvent
	WorkItem               = model.WorkItem
	WorkTracePoint         = model.WorkTracePoint
	WorkTableEvent         = model.WorkTableEvent
	WorkTableBatch         = model.WorkTableBatch
	TaskChangedEvent       = model.TaskChangedEvent
	NodeStatus             = model.NodeStatus
	Tool                   = model.Tool
	SkillInfo              = model.SkillInfo
	PluginInfo             = model.PluginInfo
	AccountInfo            = model.AccountInfo
	SessionInfo            = model.SessionInfo
	SessionMeta            = model.SessionMeta
	Interaction            = model.Interaction
	InteractionOption      = model.InteractionOption
	Capabilities           = model.Capabilities
	EventKind              = event.EventKind
	Event                  = event.Event
	MessageDelta           = event.MessageDelta
	Subscription           = event.Subscription
	EventHub               = event.EventHub
	ApprovalRequest        = approval.ApprovalRequest
	ApprovalDecision       = approval.ApprovalDecision
	ApprovalBroker         = approval.ApprovalBroker
	PromptLayer            = prompt.PromptLayer
	PromptStack            = prompt.PromptStack
	EffortManager          = prompt.EffortManager
	WindowPolicy           = cc.WindowPolicy
	ProviderContextInfo    = cc.ProviderContextInfo
	WindowConfig           = cc.WindowConfig
	DefaultWindowPolicy    = cc.DefaultWindowPolicy
)

// SessionStatus 可见状态常量（复用 model 定义）。
const (
	SessionStatusDraft            = model.SessionStatusDraft
	SessionStatusIdle             = model.SessionStatusIdle
	SessionStatusRunning          = model.SessionStatusRunning
	SessionStatusQueued           = model.SessionStatusQueued
	SessionStatusAwaitingApproval = model.SessionStatusAwaitingApproval
	SessionStatusArchived         = model.SessionStatusArchived
)

// 域子包内部类型的根包别名（保持 package core 内部调用面稳定）。
type (
	inputDispatcher    = input_router.Dispatcher
	inputRouter        = input_router.Router
	inputRouteHandlers = input_router.RouteHandlers
)

func NewCommandRegistry() *CommandRegistry { return input_router.NewCommandRegistry() }

func newInputRouter(handlers inputRouteHandlers) *inputRouter {
	return input_router.NewRouter(handlers)
}

func NewDefaultWindowPolicy(config WindowConfig) DefaultWindowPolicy {
	return cc.NewDefaultWindowPolicy(config)
}

func DefaultWindowConfig() WindowConfig {
	return cc.DefaultWindowConfig()
}

func LoadWindowConfig(path string) (WindowConfig, error) {
	return cc.LoadWindowConfig(path)
}

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
	EventChatChanged           = event.EventChatChanged
	EventWorkTableChanged      = event.EventWorkTableChanged
	EventTaskChanged           = event.EventTaskChanged
	EventInteractionOpened     = event.EventInteractionOpened
	EventInteractionClosed     = event.EventInteractionClosed
	EventError                 = event.EventError
	EventResyncRequired        = event.EventResyncRequired
	EventExitRequested         = event.EventExitRequested
	PlanPending                = model.PlanPending
	PlanRunning                = model.PlanRunning
	PlanCompleted              = model.PlanCompleted
	PlanFailed                 = model.PlanFailed
	PlanAborted                = model.PlanAborted
	NodePending                = model.NodePending
	NodeQueued                 = model.NodeQueued
	NodeRunning                = model.NodeRunning
	NodeWorktreeCreating       = model.NodeWorktreeCreating
	NodeRebasing               = model.NodeRebasing
	NodeMerging                = model.NodeMerging
	NodeCompleted              = model.NodeCompleted
	NodeFailed                 = model.NodeFailed
	NodeAborted                = model.NodeAborted
	NodeSkipped                = model.NodeSkipped
	NodeCanceled               = model.NodeCanceled
	NodePanicked               = model.NodePanicked
	TaskProgressing            = model.TaskProgressing
	TaskCompleted              = model.TaskCompleted
	TaskNeedsUserDecision      = model.TaskNeedsUserDecision
	TaskBlocked                = model.TaskBlocked
	TaskInterrupted            = model.TaskInterrupted
	TaskFailed                 = model.TaskFailed
)

var (
	ErrInteractionNotFound = approval.ErrInteractionNotFound
	ErrInteractionResolved = approval.ErrInteractionResolved
	CloneWorkItems         = model.CloneWorkItems
	CloneWorkTableBatches  = model.CloneWorkTableBatches
)

func NewEventHub() *EventHub { return event.NewEventHub() }

func NewApprovalBroker(events event.Hub) *ApprovalBroker { return approval.NewApprovalBroker(events) }

func NewPromptStack() *PromptStack { return prompt.NewPromptStack() }

func NewEffortManager(stack *PromptStack, engine interface {
	SetMaxLoops(int)
	SetSystemPrompt(string)
}) *EffortManager {
	return prompt.NewEffortManager(stack, engine)
}

type ReActBudget = prompt.ReActBudget

func cloneSnapshot(snapshot Snapshot) Snapshot { return model.CloneSnapshot(snapshot) }

func cloneRuntimeState(runtime RuntimeState) RuntimeState { return model.CloneRuntimeState(runtime) }

func maxLoopsFor(level string) int { return prompt.MaxLoops(level) }

func reactBudgetFor(level string) prompt.ReActBudget { return prompt.ReActBudgetFor(level) }
