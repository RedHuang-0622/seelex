package core

// vocabulary.go 是 core 的**词表与错误值**：会话状态、事件名、角色名、
// 协议版本等常量与包级错误值。
//
// 落点沿革：2026-10-17 随 **M5-R10b** 从 `application/core/aliases.go` 拆出。
// 同一次拆分把那个文件里的 **91 个类型别名块删掉**，
// core 内部一律改用限定名（`model.X` / `contract.Y` / `event.Z` / `input_router.W`）——
// 别名层不再做第二份命名面，门面的 A 组清单直指 `contract` 与 `middle/protocol`。
//
// 词表为何留在本包：它们是**跨文件共用的命名面**（事件名要与前端消费的
// `middle/protocol/event` 逐字一致），而不是"只为不动调用点"的兼容别名。

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

const (
	SessionStatusDraft            = model.SessionStatusDraft
	SessionStatusIdle             = model.SessionStatusIdle
	SessionStatusRunning          = model.SessionStatusRunning
	SessionStatusQueued           = model.SessionStatusQueued
	SessionStatusAwaitingApproval = model.SessionStatusAwaitingApproval
	SessionStatusArchived         = model.SessionStatusArchived
	SessionStatusUnknown          = model.SessionStatusUnknown

	// 群聊逻辑角色名（role_name）：可见消息归属与前端 EXEC/ADVISOR 展示口径，
	// provider role 仍只有 system/user/assistant/tool。
	RoleNameUser           = model.RoleNameUser
	RoleNameMain           = model.RoleNameMain
	RoleNameTL             = model.RoleNameTL
	SessionStatusRestoring = model.SessionStatusRestoring
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
	EventChatChanged           = event.EventChatChanged
	EventWorkTableChanged      = event.EventWorkTableChanged
	EventTaskChanged           = event.EventTaskChanged
	EventTeamChanged           = event.EventTeamChanged
	EventTeammateToolStarted   = event.EventTeammateToolStarted
	EventTeammateToolCompleted = event.EventTeammateToolCompleted
	EventInteractionOpened     = event.EventInteractionOpened
	EventInteractionClosed     = event.EventInteractionClosed
	EventError                 = event.EventError
	EventResyncRequired        = event.EventResyncRequired
	EventExitRequested         = event.EventExitRequested
	EventViewSessionChanged    = event.EventViewSessionChanged
	PlanUnknown                = model.PlanUnknown
	PlanPending                = model.PlanPending
	PlanRunning                = model.PlanRunning
	PlanCompleted              = model.PlanCompleted
	PlanFailed                 = model.PlanFailed
	PlanAborted                = model.PlanAborted
	NodeUnknown                = model.NodeUnknown
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
	TurnProgressing            = model.TurnProgressing
	TurnCompleted              = model.TurnCompleted
	TurnNeedsUserDecision      = model.TurnNeedsUserDecision
	TurnBlocked                = model.TurnBlocked
	TurnInterrupted            = model.TurnInterrupted
	TurnFailed                 = model.TurnFailed
	TurnUnknown                = model.TurnUnknown
	TurnIdle                   = model.TurnIdle
)

var (
	ErrInteractionNotFound = contract.ErrInteractionNotFound
	ErrInteractionResolved = contract.ErrInteractionResolved
	CloneWorkItems         = model.CloneWorkItems
	CloneWorkTableBatches  = model.CloneWorkTableBatches
)
