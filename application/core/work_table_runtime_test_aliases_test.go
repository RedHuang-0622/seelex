package core

import (
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

const (
	workTableTraceMarkerOpen  = applicationtable.TraceMarkerOpen
	workTableTraceMarkerClose = applicationtable.TraceMarkerClose
)

// Test-only aliases keep existing core integration harnesses on the public
// WorkTable runtime surface while production code calls the owner directly.
func (service *Service) refreshWorkTableLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord) {
	service.components.workTableRuntime.RefreshLocked(tasks, asyncRuns)
}

func (service *Service) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string) {
	service.components.workTableRuntime.PublishTaskChanged(record, revision, requestID, sessionID)
}

func (service *Service) syncTasksFromSources() {
	service.components.workTableRuntime.SyncFromSources()
}

func (service *Service) syncTasksFromSourcesFor(sessionID string) {
	service.components.workTableRuntime.SyncFromSourcesFor(sessionID)
}

func (service *Service) workTableTraceBlock() string {
	return service.components.workTableRuntime.TraceBlock()
}

func (service *Service) workTableTraceBlockFor(sessionID string) string {
	return service.components.workTableRuntime.TraceBlockFor(sessionID)
}

func (service *Service) refreshWorkTableSnapshot() {
	service.components.workTableRuntime.RefreshSubagents()
}

func (service *Service) refreshWorkTableFromSources() {
	service.components.workTableRuntime.RefreshFromSources()
}

func (service *Service) safeLifecycleCall(call func()) {
	service.components.workTableRuntime.SafeLifecycleCall(call)
}

func (service *Service) asyncRunsForTable() []dto.AsyncRunRecord {
	return service.components.workTableRuntime.AsyncRuns()
}
