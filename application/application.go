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
	"github.com/RedHuang-0622/seelex/application/search"
)

type (
	Service                = core.Service
	ToolHookBridge         = core.ToolHookBridge
	Suggestion             = core.Suggestion
	Command                = core.Command
	CommandResult          = core.CommandResult
	Dependencies           = contract.Dependencies
	EngineMessage          = contract.EngineMessage
	EngineToolCall         = contract.EngineToolCall
	ChatEngine             = contract.ChatEngine
	RuntimePort            = contract.RuntimePort
	PluginPort             = contract.PluginPort
	SkillPort              = contract.SkillPort
	SessionPort            = contract.SessionPort
	WorkspacePort          = contract.WorkspacePort
	Snapshot               = model.Snapshot
	SessionState           = model.SessionState
	Message                = model.Message
	ToolCall               = model.ToolCall
	ChatState              = model.ChatState
	TaskState              = model.TaskState
	ContextCompaction      = model.ContextCompaction
	ReadFileRef            = model.ReadFileRef
	SessionTitle           = model.SessionTitle
	ConversationRecord     = model.ConversationRecord
	SessionPlanFrame       = model.SessionPlanFrame
	SessionExecutionRecord = model.SessionExecutionRecord
	SessionRecord          = model.SessionRecord
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
	NodeStatus             = model.NodeStatus
	Tool                   = model.Tool
	SkillInfo              = model.SkillInfo
	PluginInfo             = model.PluginInfo
	AccountInfo            = model.AccountInfo
	SessionInfo            = model.SessionInfo
	WorkspaceInfo          = model.WorkspaceInfo
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
	WebSearchConfig        = search.WebSearchConfig
)

const (
	ProtocolVersion        = model.ProtocolVersion
	EventSnapshotChanged   = event.EventSnapshotChanged
	EventMessageAdded      = event.EventMessageAdded
	EventMessageDelta      = event.EventMessageDelta
	EventToolStarted       = event.EventToolStarted
	EventToolCompleted     = event.EventToolCompleted
	EventRuntimeChanged    = event.EventRuntimeChanged
	EventInteractionOpened = event.EventInteractionOpened
	EventInteractionClosed = event.EventInteractionClosed
	EventError             = event.EventError
	EventResyncRequired    = event.EventResyncRequired
	EventExitRequested     = event.EventExitRequested
	PlanPending            = model.PlanPending
	PlanRunning            = model.PlanRunning
	PlanCompleted          = model.PlanCompleted
	PlanFailed             = model.PlanFailed
	PlanAborted            = model.PlanAborted
	NodePending            = model.NodePending
	NodeQueued             = model.NodeQueued
	NodeRunning            = model.NodeRunning
	NodeCompleted          = model.NodeCompleted
	NodeFailed             = model.NodeFailed
	NodeAborted            = model.NodeAborted
	NodeSkipped            = model.NodeSkipped
	NodeCanceled           = model.NodeCanceled
	NodePanicked           = model.NodePanicked
)

var (
	ErrChatRunning         = core.ErrChatRunning
	ErrApplicationDraining = core.ErrApplicationDraining
	ErrInteractionNotFound = approval.ErrInteractionNotFound
	ErrInteractionResolved = approval.ErrInteractionResolved
)

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
