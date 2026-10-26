package conversation

import (
	"sync"

	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// TurnCommitDeps supplies persistence, outcome, promotion and mirror callbacks.
type TurnCommitDeps struct {
	Persist      func(sessionID, requestID string, turnErr error) (session_runtime.Location, error)
	Lock         func() sync.Locker
	Unit         func(sessionID string) *sessioncase.SessionUnit
	IsActive     func(sessionID string) bool
	ApplyOutcome func(sessionID, requestID string, unit *sessioncase.SessionUnit, projection view_state.RuntimeStateProjection, turnErr error) (string, bool)
	Promote      func(sessionID string, unit *sessioncase.SessionUnit) Promotion
	AppendNext   func(sessionID string, active bool, promotion Promotion) (*model.Message, *model.Message)
	Mirror       func(sessionID string, unit *sessioncase.SessionUnit, active bool) uint64
	Stale        func(sessionID, requestID string, unit *sessioncase.SessionUnit)
}

// CommitResult is one locked turnover result.
type CommitResult struct {
	SessionID     string
	RequestID     string
	Location      session_runtime.Location
	TurnErr       error
	VisibleError  string
	Stale         bool
	Promotion     Promotion
	NextUser      *model.Message
	NextAssistant *model.Message
	Revision      uint64
}

// TurnCommitApplication owns persistence/outcome/promotion/mirror sequencing.
type TurnCommitApplication struct {
	deps TurnCommitDeps
}

// NewTurnCommit constructs the turn-commit application.
func NewTurnCommit(deps TurnCommitDeps) *TurnCommitApplication {
	return &TurnCommitApplication{deps: deps}
}

// Commit persists, applies outcome, promotes queue and mirrors under the lock.
func (application *TurnCommitApplication) Commit(
	sessionID, requestID string,
	turnErr error,
	projection view_state.RuntimeStateProjection,
) CommitResult {
	location, settledErr := application.deps.Persist(sessionID, requestID, turnErr)
	lock := application.deps.Lock()
	lock.Lock()
	active := application.deps.IsActive(sessionID)
	unit := application.deps.Unit(sessionID)
	visibleError, stale := application.deps.ApplyOutcome(
		sessionID, requestID, unit, projection, settledErr,
	)
	if stale {
		application.deps.Stale(sessionID, requestID, unit)
		lock.Unlock()
		return CommitResult{
			SessionID: sessionID, RequestID: requestID,
			Location: location, TurnErr: settledErr, VisibleError: visibleError, Stale: true,
		}
	}
	promotion := application.deps.Promote(sessionID, unit)
	nextUser, nextAssistant := application.deps.AppendNext(sessionID, active, promotion)
	revision := application.deps.Mirror(sessionID, unit, active)
	lock.Unlock()
	return CommitResult{
		SessionID: sessionID, RequestID: requestID, Location: location,
		TurnErr: settledErr, VisibleError: visibleError, Promotion: promotion,
		NextUser: nextUser, NextAssistant: nextAssistant, Revision: revision,
	}
}
