package core

import (
	"sync"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceConversationTurnCommit(service *Service) *applicationconversation.TurnCommitApplication {
	return applicationconversation.NewTurnCommit(applicationconversation.TurnCommitDeps{
		Persist:  service.components.conversationTurnCompletion.Persist,
		Lock:     func() sync.Locker { return &service.ViewMu },
		Unit:     service.sessionUnitLocked,
		IsActive: service.isActiveSessionLocked,
		ApplyOutcome: func(sessionID, requestID string, unit *sessioncase.SessionUnit, projection view_state.RuntimeStateProjection, turnErr error) (string, bool) {
			return service.components.conversationTurnOutcome.ApplyLocked(
				sessionID, requestID, unit, projection, turnErr,
			)
		},
		Promote: func(sessionID string, unit *sessioncase.SessionUnit) applicationconversation.Promotion {
			return service.components.conversationTurnProcess.PromoteLocked(sessionID, unit)
		},
		AppendNext: func(sessionID string, active bool, promotion applicationconversation.Promotion) (*model.Message, *model.Message) {
			if !promotion.Promoted {
				return nil, nil
			}
			userOrigin := model.MessageOrigin{
				RoleName: promotion.UserEvent.RoleName, RoleSessionID: promotion.UserEvent.RoleSessionID,
				RoundID: promotion.UserEvent.RoundID, UnitSeq: promotion.UserEvent.UnitSeq, EventSeq: promotion.UserEvent.Seq,
			}
			mainOrigin := model.MessageOrigin{RoleName: model.RoleNameMain, RoleSessionID: sessionID, RoundID: promotion.UserEvent.RoundID}
			if active {
				return service.components.view.AppendMessageWithOriginLocked("user", promotion.Request.DisplayInput, nil, userOrigin),
					service.components.view.AppendMessageWithOriginLocked("assistant", "", nil, mainOrigin)
			}
			return service.components.view.AppendMessageWithOriginLockedFor(sessionID, "user", promotion.Request.DisplayInput, nil, userOrigin),
				service.components.view.AppendMessageWithOriginLockedFor(sessionID, "assistant", "", nil, mainOrigin)
		},
		Mirror: func(sessionID string, unit *sessioncase.SessionUnit, active bool) uint64 {
			return service.components.conversationTurnOutcome.MirrorLocked(sessionID, unit, active)
		},
		Stale: func(sessionID, requestID string, unit *sessioncase.SessionUnit) {
			runChatDebug(
				"runChat stale request session=%s request=%s runtimeRequest=%s (superseded)",
				sessionID, requestID, unit.ChatState().RequestID,
			)
		},
	})
}
