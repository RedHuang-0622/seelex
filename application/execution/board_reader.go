package execution

import "github.com/RedHuang-0622/seelex/contract/dto"

// BoardReader is the read-only teamwork projection consumed by WorkItem.
type BoardReader interface {
	BoardViewFor(sessionID string) *dto.TeamworkBoardView
	SessionLiveFor(sessionID string) dto.TeammateSessionLiveView
	SessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView
}
