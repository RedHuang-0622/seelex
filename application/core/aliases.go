package core

import (
	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
)

type (
	Dependencies      = contract.Dependencies
	EngineMessage     = contract.EngineMessage
	EngineToolCall    = contract.EngineToolCall
	ChatEngine        = contract.ChatEngine
	RuntimePort       = contract.RuntimePort
	PluginPort        = contract.PluginPort
	SkillPort         = contract.SkillPort
	SessionPort       = contract.SessionPort
	WorkspacePort     = contract.WorkspacePort
	WorkspaceInfo     = model.WorkspaceInfo
	Snapshot          = model.Snapshot
	SessionState      = model.SessionState
	Message           = model.Message
	ToolCall          = model.ToolCall
	ChatState         = model.ChatState
	RuntimeState      = model.RuntimeState
	ReplanMonitor     = model.ReplanMonitor
	PlanState         = model.PlanState
	PlanStatus        = model.PlanStatus
	PlanNode          = model.PlanNode
	NodeStatus        = model.NodeStatus
	Tool              = model.Tool
	SkillInfo         = model.SkillInfo
	PluginInfo        = model.PluginInfo
	AccountInfo       = model.AccountInfo
	SessionInfo       = model.SessionInfo
	Interaction       = model.Interaction
	InteractionOption = model.InteractionOption
	Capabilities      = model.Capabilities
	EventKind         = event.EventKind
	Event             = event.Event
	MessageDelta      = event.MessageDelta
	Subscription      = event.Subscription
	EventHub          = event.EventHub
	ApprovalRequest   = approval.ApprovalRequest
	ApprovalDecision  = approval.ApprovalDecision
	ApprovalBroker    = approval.ApprovalBroker
	PromptLayer       = prompt.PromptLayer
	PromptStack       = prompt.PromptStack
	EffortManager     = prompt.EffortManager
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
	ErrInteractionNotFound = approval.ErrInteractionNotFound
	ErrInteractionResolved = approval.ErrInteractionResolved
)

func NewEventHub() *EventHub { return event.NewEventHub() }

func NewApprovalBroker(events *EventHub) *ApprovalBroker { return approval.NewApprovalBroker(events) }

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
