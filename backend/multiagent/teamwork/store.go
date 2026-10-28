package teamwork

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
	workstore "github.com/RedHuang-0622/seelex/store/teamwork"
)

// planStoreRepository is the narrow consumer port implemented structurally by
// the teamwork owner. The composition root passes Stores.Teamwork directly.
type planStoreRepository interface {
	WritePlan(dto.SessionKey, workstore.Plan, int) error
	ReadPlan(dto.SessionKey) (workstore.Plan, error)
	AppendEvent(dto.SessionKey, workstore.Event) error
	ReadEvents(dto.SessionKey) ([]workstore.Event, error)
	AppendBinding(dto.SessionKey, workstore.Binding) error
	ReadBindings(dto.SessionKey) ([]workstore.Binding, error)
	JobOutputDir(dto.SessionKey) (string, error)
}

func NewPlanStore(repository planStoreRepository) PlanStore {
	if repository == nil {
		return nil
	}
	return planStore{repository: repository}
}

type planStore struct {
	repository planStoreRepository
}

func (s planStore) WritePlan(_ context.Context, key dto.SessionKey, plan workstore.Plan, maxTeammates int) error {
	return s.repository.WritePlan(key, plan, maxTeammates)
}

func (s planStore) ReadPlan(_ context.Context, key dto.SessionKey) (workstore.Plan, error) {
	return s.repository.ReadPlan(key)
}

func (s planStore) AppendEvent(_ context.Context, key dto.SessionKey, event workstore.Event) error {
	return s.repository.AppendEvent(key, event)
}

func (s planStore) ReadEvents(_ context.Context, key dto.SessionKey) ([]workstore.Event, error) {
	return s.repository.ReadEvents(key)
}

func (s planStore) AppendBinding(_ context.Context, key dto.SessionKey, binding workstore.Binding) error {
	return s.repository.AppendBinding(key, binding)
}

func (s planStore) ReadBindings(_ context.Context, key dto.SessionKey) ([]workstore.Binding, error) {
	return s.repository.ReadBindings(key)
}
