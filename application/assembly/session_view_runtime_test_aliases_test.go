package assembly

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// Test-only aliases retain the existing core integration harness while the
// Session view runtime and its state live in application/session/viewstate.
func (service *Service) projectDraftWorkspaceLocked(snapshot *protocol.Snapshot) {
	service.components.view.ProjectDraftWorkspaceLocked(snapshot)
}

func (service *Service) withLiveSessionRowsLocked(rows []protocol.SessionInfo) []protocol.SessionInfo {
	return service.components.view.WithLiveSessionRowsLocked(rows)
}

func (service *Service) enrichDirectoryRowsLocked(rows []protocol.SessionInfo) []protocol.SessionInfo {
	return service.components.view.EnrichDirectoryRowsLocked(rows)
}

func (service *Service) sessionStatusLocked(sessionID string) protocol.SessionStatus {
	return service.components.view.SessionStatusLocked(sessionID)
}

func (service *Service) isRestoringLocked(sessionID string) bool {
	return service.components.view.IsRestoringLocked(sessionID)
}

func (service *Service) setRestoringLocked(sessionID string) {
	service.components.view.SetRestoringLocked(sessionID)
}

func (service *Service) clearRestoringLocked(sessionID string) {
	service.components.view.ClearRestoringLocked(sessionID)
}

func (service *Service) restoreSignalLocked() <-chan struct{} {
	return service.components.view.RestoreSignalLocked()
}

func (service *Service) nextViewEpochLocked() uint64 {
	return service.components.view.NextViewEpochLocked()
}

func (service *Service) markSessionViewIntentLocked(sessionID string) uint64 {
	return service.components.view.MarkSessionViewIntentLocked(sessionID)
}

func (service *Service) markSessionViewIntent(sessionID string) {
	service.components.view.MarkSessionViewIntent(sessionID)
}

func (service *Service) sessionViewIntentLocked(sessionID string) uint64 {
	return service.components.view.SessionViewIntentLocked(sessionID)
}

func (service *Service) refreshRuntimeProjectionForSession(sessionID string) {
	service.components.view.RefreshRuntimeProjectionForSession(sessionID)
}

func (service *Service) appendAssistantPlaceholderAfterToolLocked(sessionID string) *protocol.Message {
	return service.components.view.AppendAssistantPlaceholderAfterToolLocked(sessionID)
}

func (service *Service) sessionViewEmptyLocked(sessionID string) bool {
	return service.components.view.SessionViewEmptyLocked(sessionID)
}

func (service *Service) recordReadFileForSessionLocked(sessionID, arguments string) {
	service.components.view.RecordReadFileForSessionLocked(sessionID, arguments)
}
