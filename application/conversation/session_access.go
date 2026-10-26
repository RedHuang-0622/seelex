package conversation

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ConversationSession is the unified main/subagent/teammate conversation face.
type ConversationSession interface {
	View(context.Context, dto.ConversationSessionRef, dto.ConversationViewRequest) (dto.SessionRecordPage, error)
	Chat(context.Context, dto.ConversationSessionRef, dto.ConversationChatRequest) (dto.ChatReceipt, error)
	Recover(context.Context, dto.ConversationSessionRef) (dto.SessionRecovery, error)
}

// SessionRecordsPort owns the one shared pagination / UTF-8 clipping rule.
type SessionRecordsPort interface {
	PaginateRecords(sessionID string, records []dto.ConversationRecord, request dto.ConversationViewRequest) dto.SessionRecordPage
}
