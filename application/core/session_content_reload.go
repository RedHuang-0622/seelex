package core

import (
	"fmt"

	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
)

// reloadSessionContent reloads an evicted visible window from durable pages.
func (service *Service) reloadSessionContent(sessionID string) error {
	window := applicationpolicy.Get().HistoryWindow
	if window <= 0 {
		window = 1
	}
	workspaceID := service.components.sessions.LocateSession(sessionID).WorkspaceID
	page, err := service.loadConversationTailPage(workspaceID, sessionID, window)
	if err != nil {
		return fmt.Errorf("reload session content %q: %w", sessionID, err)
	}
	return service.installVisibleHistory(
		sessionID,
		page,
		window,
		applicationhistory.InstallReplace,
		service.components.historyWindowAssembler,
	)
}
