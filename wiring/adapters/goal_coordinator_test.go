package adapters

import (
	"context"
	"testing"
	"time"

	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
	"github.com/RedHuang-0622/seelex/store"
)

type goalProviderFake struct {
	session *store.SessionContextStore
}

func (fake goalProviderFake) SessionContextStoreFor(string) *store.SessionContextStore {
	return fake.session
}

func TestGoalCoordinatorBridgesSessionStoreAndBoardArchive(t *testing.T) {
	router, err := store.NewRouter(t.TempDir())
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	sessionID := "session-goal-adapter"
	session := store.NewSessionContextStore(router, sessionID)
	boards, ok := router.BoardsForSession(sessionID)
	if !ok {
		t.Fatal("goal board archive is not available")
	}
	if err := boards.WriteGoalBoard(context.Background(), store.GoalBoardMeta{
		BoardLifecycle: store.BoardLifecycle{
			Kind: store.BoardKindGoal, State: store.BoardStateActive, Seq: 1,
			UpdatedAt: time.Now().UTC().Unix(),
		},
		Active: &store.GoalBoardActive{GoalID: "g-archived", Title: "archived", Status: "active"},
	}); err != nil {
		t.Fatalf("write goal board: %v", err)
	}

	coordinator := newGoalCoordinator(goalProviderFake{session: session})
	view := coordinator.GoalGovernanceViewFor(sessionID)
	if view == nil || !view.Active || !view.Recovered || view.GoalID != "g-archived" {
		t.Fatalf("recovered goal view = %+v", view)
	}

	if _, err := coordinator.Begin(context.Background(), sessionID, goaldomain.BeginRequest{Title: "live"}); err != nil {
		t.Fatalf("begin goal: %v", err)
	}
	if stack := session.GoalStackSnapshot(); len(stack) != 1 || stack[0].GoalID == "" {
		t.Fatalf("goal fifth stack was not persisted: %+v", stack)
	}
}
