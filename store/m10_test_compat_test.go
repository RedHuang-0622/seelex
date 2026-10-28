package store

// Test-only forwarding keeps legacy package-local regression cases readable
// without reintroducing production facade methods. New tests should call the
// concrete owner (repository.layout.{boards,teamwork,records}) directly.

import (
	"context"
	"encoding/json"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

type BoardRepository interface {
	WriteGoalBoard(context.Context, Key, dto.GoalBoardMeta) error
	ReadGoalBoard(context.Context, Key) (dto.GoalBoardMeta, error)
	WriteTeamBoard(context.Context, Key, dto.TeamBoardMeta) error
	ReadTeamBoard(context.Context, Key) (dto.TeamBoardMeta, error)
}

type TeamworkRepository interface {
	WriteTeamworkPlan(context.Context, Key, storeteamwork.Plan, int) error
	ReadTeamworkPlan(context.Context, Key) (storeteamwork.Plan, error)
	AppendTeamworkEvent(context.Context, Key, storeteamwork.Event) error
	ReadTeamworkEvents(context.Context, Key) ([]storeteamwork.Event, error)
	TeamworkJobOutputDir(context.Context, Key) (string, error)
	AppendTeamworkBinding(context.Context, Key, storeteamwork.Binding) error
	ReadTeamworkBindings(context.Context, Key) ([]storeteamwork.Binding, error)
}

func Boards(repository Repository) (BoardRepository, bool) {
	boards, ok := repository.(BoardRepository)
	return boards, ok
}

func Teamwork(repository Repository) (TeamworkRepository, bool) {
	work, ok := repository.(TeamworkRepository)
	return work, ok
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

func (repository *jsonRepository) ReadTeamworkPlan(_ context.Context, key Key) (storeteamwork.Plan, error) {
	return repository.layout.teamwork.ReadPlan(key)
}

func (repository *jsonRepository) ReadTeamworkEvents(_ context.Context, key Key) ([]storeteamwork.Event, error) {
	return repository.layout.teamwork.ReadEvents(key)
}

func (repository *jsonRepository) ReadGoalBoard(ctx context.Context, key Key) (dto.GoalBoardMeta, error) {
	return repository.layout.boards.ReadGoalBoard(ctx, key)
}

func (repository *jsonRepository) ReadTeamBoard(ctx context.Context, key Key) (dto.TeamBoardMeta, error) {
	return repository.layout.boards.ReadTeamBoard(ctx, key)
}

func (repository *jsonRepository) ReadToolRecordSnapshot(ctx context.Context, key Key) (json.RawMessage, bool, error) {
	raw, found, err := repository.layout.records.Read(ctx, key)
	return raw, found, err
}

func (repository *jsonRepository) WriteGoalBoard(ctx context.Context, key Key, meta dto.GoalBoardMeta) error {
	return repository.layout.boards.WriteGoalBoard(ctx, key, meta)
}

func (repository *jsonRepository) WriteTeamBoard(ctx context.Context, key Key, meta dto.TeamBoardMeta) error {
	return repository.layout.boards.WriteTeamBoard(ctx, key, meta)
}

func (repository *jsonRepository) WriteTeamworkPlan(ctx context.Context, key Key, plan storeteamwork.Plan, maxTeammates int) error {
	return repository.layout.teamwork.WritePlan(key, plan, maxTeammates)
}

func (repository *jsonRepository) AppendTeamworkEvent(_ context.Context, key Key, event storeteamwork.Event) error {
	return repository.layout.teamwork.AppendEvent(key, event)
}

func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding storeteamwork.Binding) error {
	return repository.layout.teamwork.AppendBinding(key, binding)
}

func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]storeteamwork.Binding, error) {
	return repository.layout.teamwork.ReadBindings(key)
}

func (repository *jsonRepository) TeamworkJobOutputDir(_ context.Context, key Key) (string, error) {
	return repository.layout.teamwork.JobOutputDir(key)
}

func (repository *jsonRepository) AppendToolRecordSnapshot(ctx context.Context, key Key, payload any) error {
	return repository.layout.records.Append(ctx, key, payload)
}
