package host

import (
	"context"

	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
	"github.com/RedHuang-0622/seelex/store"
)

type goalContextStoreProvider interface {
	SessionContextStoreFor(string) *store.SessionContextStore
}

var _ goalContextStoreProvider = RuntimePort{}

// NewGoalCoordinator adapts the optional runtime session-context store into
// the store-free Application Goal coordinator.
func NewGoalCoordinator(runtime contract.RuntimePort) *applicationgoal.Coordinator {
	provider, ok := runtime.(goalContextStoreProvider)
	if !ok {
		return newGoalCoordinator(nil)
	}
	return newGoalCoordinator(provider)
}

func newGoalCoordinator(provider goalContextStoreProvider) *applicationgoal.Coordinator {
	return applicationgoal.NewCoordinator(applicationgoal.CoordinatorDeps{
		StoreFor: func(sessionID string) (goaldomain.Store, goaldomain.AuditAccount) {
			adapter := goalStoreFor(provider, sessionID)
			if adapter == nil {
				return nil, nil
			}
			return adapter, adapter
		},
		ArchiveFor: func(ctx context.Context, sessionID string) (dto.GoalGovernanceView, bool) {
			adapter := goalStoreFor(provider, sessionID)
			if adapter == nil {
				return dto.GoalGovernanceView{}, false
			}
			archive, err := adapter.ReadGoalBoard(ctx)
			if err != nil {
				return dto.GoalGovernanceView{}, false
			}
			return goaldomain.GoalGovernanceViewFromBoardArchive(archive)
		},
	})
}

func goalStoreFor(provider goalContextStoreProvider, sessionID string) *goaldomain.ContextStateStore {
	if provider == nil {
		return nil
	}
	session := provider.SessionContextStoreFor(sessionID)
	if session == nil {
		return nil
	}
	var boards store.SessionBoards
	if router := session.Router(); router != nil {
		boards, _ = router.BoardsForSession(sessionID)
	}
	return goaldomain.NewContextStateStore(session, boards)
}
