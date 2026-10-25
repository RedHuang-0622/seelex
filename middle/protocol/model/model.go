// Package model is the compatibility facade for the wire protocol model.
//
// The authoritative declarations now live in contract/protocol. This package
// keeps existing middle/frontend import paths source-compatible during the
// BM-002 migration; every exported type is an identity alias and every helper
// is a one-line forwarder. It owns no state and defines no conversion rules.
package model

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

const ProtocolVersion = protocol.ProtocolVersion

type (
	AccountInfo                = protocol.AccountInfo
	ActivePlanProjection       = protocol.ActivePlanProjection
	ActiveSkill                = protocol.ActiveSkill
	Capabilities               = protocol.Capabilities
	ChatState                  = protocol.ChatState
	ComposerDraft              = protocol.ComposerDraft
	ContextCompaction          = protocol.ContextCompaction
	ConversationRecord         = protocol.ConversationRecord
	EditUserMessageForkRequest = protocol.EditUserMessageForkRequest
	EditUserMessageForkResult  = protocol.EditUserMessageForkResult
	EventRange                 = protocol.EventRange
	ForkPoint                  = protocol.ForkPoint
	ForkRequest                = protocol.ForkRequest
	Interaction                = protocol.Interaction
	InteractionOption          = protocol.InteractionOption
	Message                    = protocol.Message
	MessageOrigin              = protocol.MessageOrigin
	NodeStatus                 = protocol.NodeStatus
	PerfStats                  = protocol.PerfStats
	PlanNode                   = protocol.PlanNode
	PlanNodeEventInfo          = protocol.PlanNodeEventInfo
	PlanState                  = protocol.PlanState
	PlanStatus                 = protocol.PlanStatus
	PluginInfo                 = protocol.PluginInfo
	ProcessRuntime             = protocol.ProcessRuntime
	ProcessSnapshot            = protocol.ProcessSnapshot
	ReadFileRef                = protocol.ReadFileRef
	ReplanMonitor              = protocol.ReplanMonitor
	RuntimeState               = protocol.RuntimeState
	SessionArchive             = protocol.SessionArchive
	SessionExecutionRecord     = protocol.SessionExecutionRecord
	SessionForkRef             = protocol.SessionForkRef
	SessionInfo                = protocol.SessionInfo
	SessionInputIndex          = protocol.SessionInputIndex
	SessionInputIndexRow       = protocol.SessionInputIndexRow
	SessionInputWindow         = protocol.SessionInputWindow
	SessionMeta                = protocol.SessionMeta
	SessionPlanFrame           = protocol.SessionPlanFrame
	SessionRecord              = protocol.SessionRecord
	SessionRuntime             = protocol.SessionRuntime
	SessionSSHInfo             = protocol.SessionSSHInfo
	SessionSnapshot            = protocol.SessionSnapshot
	SessionState               = protocol.SessionState
	SessionStatus              = protocol.SessionStatus
	SessionTitle               = protocol.SessionTitle
	SkillInfo                  = protocol.SkillInfo
	Snapshot                   = protocol.Snapshot
	StoredToolResult           = protocol.StoredToolResult
	SubagentContext            = protocol.SubagentContext
	SubagentContextDecision    = protocol.SubagentContextDecision
	SubagentDetail             = protocol.SubagentDetail
	SubagentEvent              = protocol.SubagentEvent
	SubagentToolEvent          = protocol.SubagentToolEvent
	SubagentWorktreeInfo       = protocol.SubagentWorktreeInfo
	TaskChangedEvent           = protocol.TaskChangedEvent
	TaskCheckpoint             = protocol.TaskCheckpoint
	TaskContextProjection      = protocol.TaskContextProjection
	TaskState                  = protocol.TaskState
	TokenAudit                 = protocol.TokenAudit
	Tool                       = protocol.Tool
	ToolCall                   = protocol.ToolCall
	ToolRecordBoard            = protocol.ToolRecordBoard
	ToolRecordReason           = protocol.ToolRecordReason
	ToolRecordSnapshot         = protocol.ToolRecordSnapshot
	ToolRecordStat             = protocol.ToolRecordStat
	ToolResultPage             = protocol.ToolResultPage
	ToolResultRef              = protocol.ToolResultRef
	TranscriptEvent            = protocol.TranscriptEvent
	TranscriptToolCall         = protocol.TranscriptToolCall
	TurnStatus                 = protocol.TurnStatus
	VisibleOutputStream        = protocol.VisibleOutputStream
	WorkItem                   = protocol.WorkItem
	WorkTableBatch             = protocol.WorkTableBatch
	WorkTableEvent             = protocol.WorkTableEvent
	WorkTracePoint             = protocol.WorkTracePoint
	WorkspaceConnection        = protocol.WorkspaceConnection
	WorkspaceConnectionChange  = protocol.WorkspaceConnectionChange
	WorkspaceEnvironmentChange = protocol.WorkspaceEnvironmentChange
	WorkspaceEnvironmentInfo   = protocol.WorkspaceEnvironmentInfo
	WorkspaceGitInfo           = protocol.WorkspaceGitInfo
	WorkspaceInfo              = protocol.WorkspaceInfo
	WorkspaceSSHInfo           = protocol.WorkspaceSSHInfo
)

const (
	CompactionOriginAuto              = protocol.CompactionOriginAuto
	CompactionOriginExplicit          = protocol.CompactionOriginExplicit
	CompactionOriginExplicitAfterTurn = protocol.CompactionOriginExplicitAfterTurn

	TurnUnknown           = protocol.TurnUnknown
	TurnIdle              = protocol.TurnIdle
	TurnProgressing       = protocol.TurnProgressing
	TurnCompleted         = protocol.TurnCompleted
	TurnNeedsUserDecision = protocol.TurnNeedsUserDecision
	TurnBlocked           = protocol.TurnBlocked
	TurnInterrupted       = protocol.TurnInterrupted
	TurnFailed            = protocol.TurnFailed

	SessionStatusUnknown          = protocol.SessionStatusUnknown
	SessionStatusDraft            = protocol.SessionStatusDraft
	SessionStatusIdle             = protocol.SessionStatusIdle
	SessionStatusRunning          = protocol.SessionStatusRunning
	SessionStatusQueued           = protocol.SessionStatusQueued
	SessionStatusAwaitingApproval = protocol.SessionStatusAwaitingApproval
	SessionStatusArchived         = protocol.SessionStatusArchived
	SessionStatusRestoring        = protocol.SessionStatusRestoring

	RoleNameUser = protocol.RoleNameUser
	RoleNameMain = protocol.RoleNameMain
	RoleNameTL   = protocol.RoleNameTL

	PlanUnknown   = protocol.PlanUnknown
	PlanPending   = protocol.PlanPending
	PlanRunning   = protocol.PlanRunning
	PlanCompleted = protocol.PlanCompleted
	PlanFailed    = protocol.PlanFailed
	PlanAborted   = protocol.PlanAborted

	NodeUnknown          = protocol.NodeUnknown
	NodePending          = protocol.NodePending
	NodeQueued           = protocol.NodeQueued
	NodeRunning          = protocol.NodeRunning
	NodeWorktreeCreating = protocol.NodeWorktreeCreating
	NodeRebasing         = protocol.NodeRebasing
	NodeMerging          = protocol.NodeMerging
	NodeCompleted        = protocol.NodeCompleted
	NodeFailed           = protocol.NodeFailed
	NodeAborted          = protocol.NodeAborted
	NodeSkipped          = protocol.NodeSkipped
	NodeCanceled         = protocol.NodeCanceled
	NodePanicked         = protocol.NodePanicked

	TranscriptEventKindUserInput  = protocol.TranscriptEventKindUserInput
	TranscriptEventKindInternal   = protocol.TranscriptEventKindInternal
	TranscriptEventKindLLM        = protocol.TranscriptEventKindLLM
	TranscriptEventKindToolCall   = protocol.TranscriptEventKindToolCall
	TranscriptEventKindToolOutput = protocol.TranscriptEventKindToolOutput
	TranscriptEventKindSystem     = protocol.TranscriptEventKindSystem
	TranscriptEventKindError      = protocol.TranscriptEventKindError
	TranscriptEventKindNotice     = protocol.TranscriptEventKindNotice

	TaskContextProjectionSchemaVersion = protocol.TaskContextProjectionSchemaVersion
)

func ExplicitCompactionOrigin(origin string) bool {
	return protocol.ExplicitCompactionOrigin(origin)
}

func CompactionRangeLabel(messageFrom, messageTo string, eventFrom, eventTo uint64) string {
	return protocol.CompactionRangeLabel(messageFrom, messageTo, eventFrom, eventTo)
}

func CloneWorkItems(items []WorkItem) []WorkItem {
	return protocol.CloneWorkItems(items)
}

func CloneSnapshot(snapshot Snapshot) Snapshot {
	return protocol.CloneSnapshot(snapshot)
}

func CloneSessionForkRef(ref *SessionForkRef) *SessionForkRef {
	return protocol.CloneSessionForkRef(ref)
}

func CloneRuntimeState(runtime RuntimeState) RuntimeState {
	return protocol.CloneRuntimeState(runtime)
}

func CloneTeamworkBoardView(view *dto.TeamworkBoardView) *dto.TeamworkBoardView {
	return protocol.CloneTeamworkBoardView(view)
}

func CloneWorkTableBatches(batches []WorkTableBatch) []WorkTableBatch {
	return protocol.CloneWorkTableBatches(batches)
}

func ToolCallStatusOfRecord(word string) dto.ToolEventStatus {
	return protocol.ToolCallStatusOfRecord(word)
}

func NewVisibleOutputStream(requestID string) *VisibleOutputStream {
	return protocol.NewVisibleOutputStream(requestID)
}

func StripThoughtBlocks(value string) string {
	return protocol.StripThoughtBlocks(value)
}
