package core

import (
	"context"

	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// newServiceSessionQuery adapts Service's authoritative directory, metadata,
// workspace and durable read hosts to the read-only session query package.
func newServiceSessionQuery(service *Service) *applicationsessionquery.Application {
	return applicationsessionquery.New(applicationsessionquery.Deps{
		ViewMu: &service.ViewMu,
		CatalogRows: func() []model.SessionInfo {
			return service.Core.Snapshot.Sessions
		},
		WithLiveRows: service.withLiveSessionRowsLocked,
		EnrichRows:   service.enrichDirectoryRowsLocked,
		Meta:         service.metaDeps,
		Workspace:    service.Deps.Workspace,
		WaitCatalog: func(ctx context.Context) error {
			return service.components.residency.WaitCatalogRefresh(ctx, service.components.sessions)
		},
		SnapshotOf: service.readSnapshotOf,
		Transcript: service.readSessionTranscript,
		InputIndex: service.readSessionInputIndex,
	})
}
