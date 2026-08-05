package core

import (
	"sort"
	"strings"
	"time"
)

const sessionCatalogShutdownWait = 100 * time.Millisecond

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

// startSessionCatalogRefresh moves catalog discovery and title restoration off
// the Snapshot hot path. The worker owns external SessionPort/WorkspacePort
// calls; it publishes only a copied in-memory projection.
func (service *Service) startSessionCatalogRefresh() {
	go func() {
		defer close(service.sessionCatalogDone)
		for {
			select {
			case <-service.sessionCatalogWake:
				service.refreshSessionCatalogCache()
			case <-service.sessionCatalogStop:
				return
			}
		}
	}()
	service.requestSessionCatalogRefresh()
}

func (service *Service) requestSessionCatalogRefresh() {
	select {
	case service.sessionCatalogWake <- struct{}{}:
	default:
	}
}

func (service *Service) refreshSessionCatalogCache() {
	sessions, discoveredBindings := service.components.sessions.sessionCatalog()
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return
	}
	service.snapshot.Sessions = append([]SessionInfo(nil), sessions...)
	bindings := make(map[string]string, len(service.snapshot.SessionWorkspaces)+len(discoveredBindings))
	for sessionID, workspaceID := range service.snapshot.SessionWorkspaces {
		bindings[sessionID] = workspaceID
	}
	for sessionID, workspaceID := range discoveredBindings {
		bindings[sessionID] = workspaceID
	}
	service.snapshot.SessionWorkspaces = bindings
	if service.snapshot.Session.Name == "" {
		for _, session := range sessions {
			if session.ID == service.snapshot.Session.ID {
				service.snapshot.Session.Name = session.Name
				break
			}
		}
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}

func (service *Service) stopSessionCatalogRefresh() {
	service.sessionCatalogOnce.Do(func() {
		close(service.sessionCatalogStop)
		// SessionPort deliberately predates context-aware catalog reads. Do not
		// turn a slow external catalog operation into a GUI shutdown hang; the
		// worker checks service.closed before publishing and exits once the port
		// returns. Normal workers are still drained eagerly.
		select {
		case <-service.sessionCatalogDone:
		case <-time.After(sessionCatalogShutdownWait):
		}
	})
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
		name = service.sessionNameFromTail(location, scoped)
	}
	service.sessionNameMu.Lock()
	service.sessionNames[key] = sessionNameCacheEntry{updatedAt: location.meta.UpdatedAt, name: name}
	service.sessionNameMu.Unlock()
	return name
}

func (service *sessionCoordinator) sessionNameFromTail(location sessionLocation, scoped scopedSessionPort) string {
	window := Limits().HistoryWindow
	_, total, err := scoped.LoadHistoryRangeWorkspace(location.workspaceID, location.meta.ID, 0, 0)
	if err != nil {
		return ""
	}
	offset := total - window
	if offset < 0 {
		offset = 0
	}
	history, _, err := scoped.LoadHistoryRangeWorkspace(location.workspaceID, location.meta.ID, offset, window)
	if err != nil {
		return ""
	}
	return sessionTitleFromHistory(history)
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
	maxRunes := Limits().SessionNameRunes // limits.session_name_runes（默认 16）
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes])
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
