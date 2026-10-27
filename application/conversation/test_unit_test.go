package conversation_test

import (
	"context"

	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type queuePromotionTestUnit struct {
	chat     model.ChatState
	requests []sessioncase.QueuedRequest
	cancel   context.CancelFunc
}

func (unit *queuePromotionTestUnit) ChatState() model.ChatState { return unit.chat }

func (unit *queuePromotionTestUnit) SetChatState(chat model.ChatState, _ *sessioncase.View) {
	unit.chat = chat
}

func (unit *queuePromotionTestUnit) UpdateChat(update func(*model.ChatState), _ *sessioncase.View) {
	update(&unit.chat)
}

func (unit *queuePromotionTestUnit) QueueProjection() ([]string, int) {
	return nil, len(unit.requests)
}

func (unit *queuePromotionTestUnit) SetCancel(cancel context.CancelFunc) {
	unit.cancel = cancel
}

func (unit *queuePromotionTestUnit) SetStream(sessioncase.VisibleOutputSink) {}

func (unit *queuePromotionTestUnit) PendingRequests() []sessioncase.QueuedRequest {
	return unit.requests
}

func (unit *queuePromotionTestUnit) SetRequests(requests []sessioncase.QueuedRequest) {
	unit.requests = requests
}
