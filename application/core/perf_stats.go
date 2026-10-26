package core

import (
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServicePerfStatsApplication(service *Service) *applicationobservability.PerfApplication {
	return applicationobservability.NewPerfApplication(applicationobservability.PerfDeps{
		LockView:   service.ViewMu.RLock,
		UnlockView: service.ViewMu.RUnlock,
		Snapshot: func() model.Snapshot {
			return service.Core.Snapshot
		},
		HistoryWindow: func() int {
			return applicationpolicy.Get().HistoryWindow
		},
		ToolResultRefs: func() []model.ToolResultRef {
			return service.components.tasks.ToolResultRefs()
		},
	})
}
