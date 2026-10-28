package adapters

import (
	"context"
	"testing"
	"time"

	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
	"github.com/RedHuang-0622/seelex/store"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
)

type goalProviderFake struct {
	session *store.SessionContextStore
	boards  *storeboard.BoundStore
}

func (fake goalProviderFake) SessionContextStoreFor(string) *store.SessionContextStore {
	return fake.session
}

func (fake goalProviderFake) SessionBoardsFor(string) *storeboard.BoundStore {
	return fake.boards
}

func TestGoalCoordinatorBridgesSessionStoreAndBoardArchive(t *testing.T) {
	stores, err := store.OpenStores(t.TempDir())
	if err != nil {
		t.Fatalf("open stores: %v", err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	router := stores.Router

	sessionID := "session-goal-adapter"
	session := store.NewSessionContextStore(router, sessionID)
	boards := stores.Boards.Bind(store.Key{SessionID: sessionID})
	if err := boards.WriteGoalBoard(context.Background(), store.GoalBoardMeta{
		BoardLifecycle: store.BoardLifecycle{
			Kind: store.BoardKindGoal, State: store.BoardStateActive, Seq: 1,
			UpdatedAt: time.Now().UTC().Unix(),
		},
		Active: &store.GoalBoardActive{GoalID: "g-archived", Title: "archived", Status: "active"},
	}); err != nil {
		t.Fatalf("write goal board: %v", err)
	}

	coordinator := newGoalCoordinator(goalProviderFake{session: session, boards: boards})
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
