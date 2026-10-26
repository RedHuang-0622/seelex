// Package observability contains application adapters for read-only runtime
// metrics. Calculations remain in the middle-layer view package.
package observability

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	middleview "github.com/RedHuang-0622/seelex/middle/view"
)

// PerfDeps supplies the view lock and the three bounded metric inputs.
type PerfDeps struct {
	LockView       func()
	UnlockView     func()
	Snapshot       func() model.Snapshot
	HistoryWindow  func() int
	ToolResultRefs func() []model.ToolResultRef
}

// PerfApplication coordinates the read lock around the authoritative metric
// calculation. It does not own snapshots, limits or tool-result state.
type PerfApplication struct {
	deps PerfDeps
}

// NewPerfApplication constructs the performance metrics application.
func NewPerfApplication(deps PerfDeps) *PerfApplication {
	return &PerfApplication{deps: deps}
}

// PerfStats returns content-free performance metrics for the GUI poller.
func (application *PerfApplication) PerfStats() model.PerfStats {
	application.deps.LockView()
	defer application.deps.UnlockView()
	return middleview.PerfStats(
		application.deps.Snapshot(),
		application.deps.HistoryWindow(),
		application.deps.ToolResultRefs(),
	)
}
