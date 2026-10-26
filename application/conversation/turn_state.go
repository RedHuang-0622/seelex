package conversation

import (
	"time"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	sessioncase "github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

// ChatStateUnit is the SessionUnit surface used by ChatState transitions.
type ChatStateUnit interface {
	ChatState() model.ChatState
	SetChatState(model.ChatState, *sessioncase.View)
	QueueProjectionUnit
}

// TurnStateApplication owns ChatState transition rules; SessionUnit remains
// the mutable data owner.
type TurnStateApplication struct{}

// NewTurnState constructs the turn-state application.
func NewTurnState() *TurnStateApplication {
	return &TurnStateApplication{}
}

// BeginLocked starts one active turn.
func (*TurnStateApplication) BeginLocked(unit ChatStateUnit, requestID string) {
	unit.SetChatState(model.ChatState{
		Running: true, RequestID: requestID, StartedAt: time.Now(),
	}, nil)
}

// ProjectQueueLocked projects queued inputs into ChatState.
func (*TurnStateApplication) ProjectQueueLocked(unit QueueProjectionUnit) ([]string, int) {
	displays, count := unit.QueueProjection()
	unit.UpdateChat(func(chat *model.ChatState) {
		chat.InputQueue = displays
		chat.QueuedCount = count
	}, nil)
	return displays, count
}

// ClearErrorLocked removes the previous visible error.
func (*TurnStateApplication) ClearErrorLocked(unit ChatStateUnit) {
	unit.UpdateChat(func(chat *model.ChatState) { chat.Error = "" }, nil)
}

// SetErrorLocked records the current visible error.
func (*TurnStateApplication) SetErrorLocked(unit ChatStateUnit, visibleError string) {
	unit.UpdateChat(func(chat *model.ChatState) { chat.Error = visibleError }, nil)
}

// FinishLocked ends the active turn.
func (*TurnStateApplication) FinishLocked(unit ChatStateUnit) {
	unit.UpdateChat(func(chat *model.ChatState) { chat.Running = false }, nil)
}

// ResetLocked returns ChatState to empty.
func (*TurnStateApplication) ResetLocked(unit ChatStateUnit) {
	unit.SetChatState(model.ChatState{}, nil)
}
