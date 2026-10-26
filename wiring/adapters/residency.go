package adapters

import (
	"context"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	middleresidency "github.com/RedHuang-0622/seelex/middle/residency"
)

type residencyPort struct{}

var _ applicationsession.ResidencyPort = residencyPort{}

// NewResidencyPort exposes the Middle residency decisions through the
// application-owned consumer port. No decision is reimplemented here.
func NewResidencyPort() applicationsession.ResidencyPort { return residencyPort{} }

func (residencyPort) Preview(content string, limit int) (string, bool) {
	return middleresidency.Preview(content, limit)
}

func (residencyPort) Touch(order []string, sessionID string) []string {
	return middleresidency.Order(order).Touch(sessionID)
}

func (residencyPort) Forget(order []string, sessionID string) []string {
	return middleresidency.Order(order).Forget(sessionID)
}

func (residencyPort) Prune(order []string, loaded func(string) bool, unitIDs []string) []string {
	return middleresidency.PruneOrder(middleresidency.Order(order), loaded, unitIDs)
}

func (residencyPort) Budget(legacyLimit, protectedCount, window int) int {
	return middleresidency.Budget(legacyLimit, protectedCount, window)
}

func (residencyPort) InputIndexSummaryLimit() int { return middleresidency.InputIndexSummaryLimit }

func (residencyPort) BuildInputIndex(
	conversation []model.Message,
	loaded []string,
	limit int,
	userText func(model.Message) string,
) []model.SessionInputIndexRow {
	return middleresidency.BuildInputIndex(conversation, loaded, limit, userText)
}

func (residencyPort) WaitCatalogRefresh(ctx context.Context, refresher applicationsession.CatalogRefresher) error {
	return middleresidency.WaitCatalogRefresh(ctx, refresher)
}

func (residencyPort) Evict(deps applicationsession.EvictionDeps) {
	middleresidency.EvictUntilWithinLimit(middleresidency.EvictionDeps{
		Snapshot: func() middleresidency.EvictionSnapshot {
			snapshot := deps.Snapshot()
			return middleresidency.EvictionSnapshot{
				Order:       middleresidency.Order(snapshot.Order),
				Protect:     snapshot.Protect,
				Occupied:    snapshot.Occupied,
				Protected:   snapshot.Protected,
				LegacyLimit: snapshot.LegacyLimit,
				Window:      snapshot.Window,
			}
		},
		Evict:        deps.Evict,
		OnEvictError: deps.OnEvictError,
	})
}

func (residencyPort) Archive(deps applicationsession.ArchiveUseCaseDeps, sessionID string) error {
	return middleresidency.Archive(middleresidency.ArchiveDeps{
		Gate: func() middleresidency.ArchiveGate {
			gate := deps.Gate()
			return middleresidency.ArchiveGate{
				Closed:      gate.Closed,
				Draining:    gate.Draining,
				Busy:        gate.Busy,
				Restoring:   gate.Restoring,
				ErrClosed:   gate.ErrClosed,
				ErrDraining: gate.ErrDraining,
				ErrBusy:     gate.ErrBusy,
			}
		},
		Store: middleresidency.ArchiveStore{
			Load:           deps.Store.Load,
			Flush:          deps.Store.Flush,
			MarkArchived:   deps.Store.MarkArchived,
			RefreshProject: deps.Store.RefreshProject,
		},
		Engine: middleresidency.ArchiveEngine{
			Resident:       deps.Engine.Resident,
			Loaded:         deps.Engine.Loaded,
			Unload:         deps.Engine.Unload,
			ForgetResident: deps.Engine.ForgetResident,
		},
		ReleaseAsync: deps.ReleaseAsync,
	}, sessionID)
}
