package core

import (
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func (service *Service) perfStatsApplicationComponent() *applicationobservability.PerfApplication {
	if service == nil {
		return nil
	}
	return service.components.perfStatsApplication
}

func newServicePerfStatsApplication(service *Service) *applicationobservability.PerfApplication {
	return applicationobservability.NewPerfApplication(applicationobservability.PerfDeps{
		LockView:   service.ViewMu.RLock,
		UnlockView: service.ViewMu.RUnlock,
		Snapshot: func() model.Snapshot {
			return service.Core.Snapshot
		},
		HistoryWindow: func() int {
			return Limits().HistoryWindow
		},
		ToolResultRefs: func() []model.ToolResultRef {
			return service.components.tasks.ToolResultRefs()
		},
	})
}
