package store

// Test-only forwarding keeps legacy package-local regression cases readable
// without reintroducing production facade methods. New tests should call the
// concrete owner (repository.layout.{boards,teamwork,records}) directly.

import (
	"context"
	"encoding/json"
)

type BoardRepository interface {
	WriteGoalBoard(context.Context, Key, GoalBoardMeta) error
	ReadGoalBoard(context.Context, Key) (GoalBoardMeta, error)
	WriteTeamBoard(context.Context, Key, TeamBoardMeta) error
	ReadTeamBoard(context.Context, Key) (TeamBoardMeta, error)
}

type TeamworkRepository interface {
	WriteTeamworkPlan(context.Context, Key, TeamworkPlan, int) error
	ReadTeamworkPlan(context.Context, Key) (TeamworkPlan, error)
	AppendTeamworkEvent(context.Context, Key, TeamworkEvent) error
	ReadTeamworkEvents(context.Context, Key) ([]TeamworkEvent, error)
	TeamworkJobOutputDir(context.Context, Key) (string, error)
	AppendTeamworkBinding(context.Context, Key, TeamworkBinding) error
	ReadTeamworkBindings(context.Context, Key) ([]TeamworkBinding, error)
}

func Boards(repository Repository) (BoardRepository, bool) {
	boards, ok := repository.(BoardRepository)
	return boards, ok
}

func Teamwork(repository Repository) (TeamworkRepository, bool) {
	work, ok := repository.(TeamworkRepository)
	return work, ok
}

func MediaStoreOf(repository Repository) (MediaStore, bool) {
	media, ok := repository.(MediaStore)
	return media, ok
}

func (router *Router) BoardsFor() (BoardRepository, bool) {
	if router == nil {
		return nil, false
	}
	var boards BoardRepository
	var ok bool
	_ = router.withRepository(func(repository Repository, _ string) error {
		boards, ok = Boards(repository)
		return nil
	})
	return boards, ok
}

func (repository *jsonRepository) ReadTeamworkPlan(_ context.Context, key Key) (TeamworkPlan, error) {
	return repository.layout.teamwork.ReadPlan(key)
}

func (repository *jsonRepository) ReadTeamworkEvents(_ context.Context, key Key) ([]TeamworkEvent, error) {
	return repository.layout.teamwork.ReadEvents(key)
}

func (repository *jsonRepository) ReadGoalBoard(ctx context.Context, key Key) (GoalBoardMeta, error) {
	return repository.layout.boards.ReadGoalBoard(ctx, key)
}

func (repository *jsonRepository) ReadTeamBoard(ctx context.Context, key Key) (TeamBoardMeta, error) {
	return repository.layout.boards.ReadTeamBoard(ctx, key)
}

func (repository *jsonRepository) ReadToolRecordSnapshot(ctx context.Context, key Key) (json.RawMessage, bool, error) {
	raw, found, err := repository.layout.records.Read(ctx, key)
	return raw, found, err
}

func (repository *jsonRepository) WriteGoalBoard(ctx context.Context, key Key, meta GoalBoardMeta) error {
	return repository.layout.boards.WriteGoalBoard(ctx, key, meta)
}

func (repository *jsonRepository) WriteTeamBoard(ctx context.Context, key Key, meta TeamBoardMeta) error {
	return repository.layout.boards.WriteTeamBoard(ctx, key, meta)
}

func (repository *jsonRepository) WriteTeamworkPlan(ctx context.Context, key Key, plan TeamworkPlan, maxTeammates int) error {
	return repository.layout.teamwork.WritePlan(key, plan, maxTeammates)
}

func (repository *jsonRepository) AppendTeamworkEvent(_ context.Context, key Key, event TeamworkEvent) error {
	return repository.layout.teamwork.AppendEvent(key, event)
}

func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding TeamworkBinding) error {
	return repository.layout.teamwork.AppendBinding(key, binding)
}

func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]TeamworkBinding, error) {
	return repository.layout.teamwork.ReadBindings(key)
}

func (repository *jsonRepository) TeamworkJobOutputDir(_ context.Context, key Key) (string, error) {
	return repository.layout.teamwork.JobOutputDir(key)
}

func (repository *jsonRepository) AppendToolRecordSnapshot(ctx context.Context, key Key, payload any) error {
	return repository.layout.records.Append(ctx, key, payload)
}
