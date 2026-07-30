package core

import (
	"sort"
	"strings"
	"time"
)

// scopedSessionPort is implemented by production session adapters that can
// read an explicit project key without mutating the active write scope.
type scopedSessionPort interface {
	ListWorkspace(workspaceID string) []SessionInfo
	LoadHistoryWorkspace(workspaceID, sessionID string) ([]EngineMessage, error)
	LoadHistoryRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]EngineMessage, int, error)
	DeleteWorkspace(workspaceID, sessionID string) error
}

type sessionLocation struct {
	workspaceID string
	workspace   *WorkspaceInfo
	meta        SessionInfo
}

type sessionNameCacheEntry struct {
	updatedAt time.Time
	name      string
}

func (service *sessionCoordinator) sessionCatalog() ([]SessionInfo, map[string]string) {
	scoped, ok := service.deps.Sessions.(scopedSessionPort)
	if !ok {
		return service.deps.Sessions.List(), nil
	}

	locations := service.allSessionLocations(scoped)
	bindings := map[string]string{}
	if service.deps.Workspace != nil {
		bindings = service.deps.Workspace.AllBindings()
	}
	selected := make(map[string]sessionLocation, len(locations))
	for _, location := range locations {
		current, exists := selected[location.meta.ID]
		if !exists || preferSessionLocation(location, current, bindings[location.meta.ID]) {
			selected[location.meta.ID] = location
		}
	}

	sessions := make([]SessionInfo, 0, len(selected))
	discovered := make(map[string]string, len(selected))
	for sessionID, location := range selected {
		meta := location.meta
		if meta.Name == "" {
			meta.Name = service.sessionName(location, scoped)
		}
		sessions = append(sessions, meta)
		if location.workspaceID != "" {
			discovered[sessionID] = location.workspaceID
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, discovered
}

func (service *sessionCoordinator) sessionName(location sessionLocation, scoped scopedSessionPort) string {
	key := location.workspaceID + "\x00" + location.meta.ID
	service.sessionNameMu.Lock()
	entry, ok := service.sessionNames[key]
	service.sessionNameMu.Unlock()
	if ok && entry.updatedAt.Equal(location.meta.UpdatedAt) {
		return entry.name
	}

	name := ""
	if store, ok := service.deps.Sessions.(sessionRecordPort); ok {
		record, err := store.LoadSessionRecordWorkspace(location.workspaceID, location.meta.ID)
		if err == nil {
			name = strings.TrimSpace(record.Title.Value)
		}
	}
	if name == "" {
		history, err := scoped.LoadHistoryWorkspace(location.workspaceID, location.meta.ID)
		if err == nil {
			name = sessionTitleFromHistory(history)
		}
	}
	service.sessionNameMu.Lock()
	service.sessionNames[key] = sessionNameCacheEntry{updatedAt: location.meta.UpdatedAt, name: name}
	service.sessionNameMu.Unlock()
	return name
}

func (service *sessionCoordinator) invalidateSessionName(sessionID string) {
	suffix := "\x00" + strings.TrimSpace(sessionID)
	service.sessionNameMu.Lock()
	for key := range service.sessionNames {
		if strings.HasSuffix(key, suffix) {
			delete(service.sessionNames, key)
		}
	}
	service.sessionNameMu.Unlock()
}

func (service *sessionCoordinator) clearSessionNames() {
	service.sessionNameMu.Lock()
	clear(service.sessionNames)
	service.sessionNameMu.Unlock()
}

func sessionTitleFromHistory(history []EngineMessage) string {
	for _, message := range history {
		if message.Role != "user" {
			continue
		}
		if title := sessionTitle(displayUserInput(message.Content)); title != "" {
			return title
		}
	}
	return ""
}

func sessionTitle(input string) string {
	for _, line := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 48 {
			return string(runes[:47]) + "…"
		}
		return line
	}
	return ""
}

func shortSessionID(id string) string {
	runes := []rune(strings.TrimSpace(id))
	if len(runes) <= 16 {
		return string(runes)
	}
	return string(runes[:16])
}

func (service *sessionCoordinator) allSessionLocations(scoped scopedSessionPort) []sessionLocation {
	locations := make([]sessionLocation, 0)
	for _, meta := range scoped.ListWorkspace("") {
		locations = append(locations, sessionLocation{meta: meta})
	}
	if service.deps.Workspace == nil {
		return locations
	}
	for _, item := range service.deps.Workspace.List() {
		workspace := item
		for _, meta := range scoped.ListWorkspace(item.ID) {
			locations = append(locations, sessionLocation{workspaceID: item.ID, workspace: &workspace, meta: meta})
		}
	}
	return locations
}

func (service *sessionCoordinator) locateSession(sessionID string) sessionLocation {
	sessionID = strings.TrimSpace(sessionID)
	var boundWorkspace *WorkspaceInfo
	if service.deps.Workspace != nil {
		if workspace, ok := service.deps.Workspace.SessionWorkspace(sessionID); ok {
			boundWorkspace = &workspace
		}
	}

	scoped, ok := service.deps.Sessions.(scopedSessionPort)
	if !ok {
		location := sessionLocation{meta: SessionInfo{ID: sessionID}}
		if boundWorkspace != nil {
			location.workspaceID = boundWorkspace.ID
			location.workspace = boundWorkspace
		}
		return location
	}

	var selected sessionLocation
	found := false
	for _, location := range service.allSessionLocations(scoped) {
		if location.meta.ID != sessionID {
			continue
		}
		if !found || preferSessionLocation(location, selected, workspaceID(boundWorkspace)) {
			selected = location
			found = true
		}
	}
	if found {
		return selected
	}
	if boundWorkspace != nil {
		return sessionLocation{workspaceID: boundWorkspace.ID, workspace: boundWorkspace, meta: SessionInfo{ID: sessionID}}
	}
	return sessionLocation{meta: SessionInfo{ID: sessionID}}
}

func preferSessionLocation(candidate, current sessionLocation, boundWorkspaceID string) bool {
	if boundWorkspaceID != "" {
		if candidate.workspaceID == boundWorkspaceID && current.workspaceID != boundWorkspaceID {
			return true
		}
		if current.workspaceID == boundWorkspaceID && candidate.workspaceID != boundWorkspaceID {
			return false
		}
	}
	return candidate.meta.UpdatedAt.After(current.meta.UpdatedAt)
}

func workspaceID(workspace *WorkspaceInfo) string {
	if workspace == nil {
		return ""
	}
	return workspace.ID
}

func (service *sessionCoordinator) loadSessionHistory(location sessionLocation, sessionID string) ([]EngineMessage, error) {
	if scoped, ok := service.deps.Sessions.(scopedSessionPort); ok {
		return scoped.LoadHistoryWorkspace(location.workspaceID, sessionID)
	}
	previous := service.deps.Sessions.Workspace()
	service.deps.Sessions.SetWorkspace(location.workspaceID)
	history, err := service.deps.Sessions.LoadHistory(sessionID)
	if err != nil {
		service.deps.Sessions.SetWorkspace(previous)
	}
	return history, err
}

func (service *sessionCoordinator) loadSessionHistoryRange(workspaceID, sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	if scoped, ok := service.deps.Sessions.(scopedSessionPort); ok {
		return scoped.LoadHistoryRangeWorkspace(workspaceID, sessionID, offset, limit)
	}
	return service.deps.Sessions.LoadHistoryRange(sessionID, offset, limit)
}
