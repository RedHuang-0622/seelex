package query

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

// ResidentSnapshotInput is the immutable input for one resident snapshot.
type ResidentSnapshotInput struct {
	ProtocolVersion      int
	Revision             uint64
	ConversationRevision uint64
	Session              model.SessionState
	Conversation         []model.Message
	Chat                 model.ChatState
	Runtime              model.SessionRuntime
	Capabilities         model.Capabilities
	HistoryOffset        int
	TotalMessages        int
	HasMoreHistory       bool
	ConversationWindow   int
	ReadFiles            []model.ReadFileRef
	ReviewRequest        *dto.ReviewRequest
	Approvals            []model.Interaction
	Plan                 *model.PlanState
	Task                 *model.TaskState
	TaskRecords          []dto.TaskRecord
	AsyncRuns            []dto.AsyncRunRecord
	Limits               worktable.Limits
	AnnotateConversation func([]model.Message, bool) []model.Message
}

// BuildResidentSnapshot assembles a detached resident session snapshot.
func BuildResidentSnapshot(input ResidentSnapshotInput) model.SessionSnapshot {
	session := input.Session
	session.ForkedFrom = model.CloneSessionForkRef(input.Session.ForkedFrom)
	snapshot := model.SessionSnapshot{
		ProtocolVersion:      input.ProtocolVersion,
		Revision:             input.Revision,
		ConversationRevision: input.ConversationRevision,
		Session:              session,
		Conversation:         append([]model.Message(nil), input.Conversation...),
		Chat:                 input.Chat,
		Runtime:              input.Runtime,
		Capabilities:         input.Capabilities,
		Resident:             true,
		HistoryOffset:        input.HistoryOffset,
		TotalMessages:        input.TotalMessages,
		HasMoreHistory:       input.HasMoreHistory,
		ConversationWindow:   input.ConversationWindow,
		ReadFiles:            append([]model.ReadFileRef(nil), input.ReadFiles...),
		ReviewRequest:        dto.CloneReviewRequest(input.ReviewRequest),
	}
	if input.Approvals != nil {
		snapshot.Approvals = append([]model.Interaction(nil), input.Approvals...)
	}
	if input.AnnotateConversation != nil {
		snapshot.Conversation = input.AnnotateConversation(snapshot.Conversation, input.Chat.Running)
	}
	if input.Plan != nil {
		snapshot.Runtime.Plan = worktable.ClonePlanForSync(input.Plan)
	}
	if input.Task != nil {
		task := *input.Task
		task.ContextCompactions = append([]model.ContextCompaction(nil), input.Task.ContextCompactions...)
		snapshot.Task = &task
	}
	if len(input.TaskRecords) > 0 || len(input.AsyncRuns) > 0 {
		rows := worktable.BuildWorkTable(snapshot.Runtime.Plan, input.TaskRecords, nil, input.AsyncRuns, input.Limits)
		snapshot.Runtime.WorkTable = rows
		snapshot.Runtime.WorkTableBatches = worktable.BuildWorkTableBatches(rows)
	}
	return snapshot
}
