package adapters

import (
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract/dto"
	sessionaccess "github.com/RedHuang-0622/seelex/middle/view/sessionaccess"
)

type sessionRecordsPort struct{}

var _ applicationconversation.SessionRecordsPort = sessionRecordsPort{}

// NewSessionRecordsPort exposes the shared session-record paginator.
func NewSessionRecordsPort() applicationconversation.SessionRecordsPort {
	return sessionRecordsPort{}
}

func (sessionRecordsPort) PaginateRecords(
	sessionID string,
	records []dto.ConversationRecord,
	request dto.ConversationViewRequest,
) dto.SessionRecordPage {
	return sessionaccess.PaginateRecords(sessionID, records, request)
}
