package conversation

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/event"
)

// Publication is the post-lock turn event payload.
type Publication struct {
	SessionID     string
	RequestID     string
	Revision      uint64
	VisibleError  string
	TurnErr       error
	Promotion     Promotion
	NextUser      *model.Message
	NextAssistant *model.Message
}

// TurnPublisherDeps supplies event and projection publication callbacks.
type TurnPublisherDeps struct {
	PublishChatState          func(sessionID string)
	PublishEvent              func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any)
	SetCurrentTaskBatch       func(sessionID, requestID string)
	PublishRuntimeProjections func()
}

// TurnPublisherApplication owns the post-lock publication branch.
type TurnPublisherApplication struct {
	deps TurnPublisherDeps
}

// NewTurnPublisher constructs the turn publisher.
func NewTurnPublisher(deps TurnPublisherDeps) *TurnPublisherApplication {
	return &TurnPublisherApplication{deps: deps}
}

// Publish emits chat/snapshot/error and promoted-message events.
func (application *TurnPublisherApplication) Publish(publication Publication) {
	application.deps.PublishChatState(publication.SessionID)
	if publication.TurnErr != nil {
		application.deps.PublishEvent(
			event.EventError, publication.Revision, publication.RequestID,
			publication.SessionID, map[string]string{"message": publication.VisibleError},
		)
	} else {
		application.deps.PublishEvent(
			event.EventSnapshotChanged, publication.Revision, publication.RequestID,
			publication.SessionID, nil,
		)
	}
	if !publication.Promotion.Promoted {
		return
	}
	application.deps.PublishEvent(
		event.EventMessageAdded, publication.Revision, publication.Promotion.RequestID,
		publication.SessionID, *publication.NextUser,
	)
	application.deps.PublishEvent(
		event.EventMessageAdded, publication.Revision, publication.Promotion.RequestID,
		publication.SessionID, *publication.NextAssistant,
	)
	application.deps.SetCurrentTaskBatch(publication.SessionID, publication.Promotion.RequestID)
	application.deps.PublishRuntimeProjections()
}
