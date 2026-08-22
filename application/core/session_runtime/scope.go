package session_runtime

import (
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
)

// sessionCatalog 返回可见会话列表与工作区绑定发现结果。
func (c *Coordinator) sessionCatalog() ([]model.SessionInfo, map[string]string) {
	scoped, ok := c.Core.Deps.Sessions.(ScopedSessionPort)
	if !ok {
		return c.Core.Deps.Sessions.List(), nil
	}

	locations := c.allSessionLocations(scoped)
	bindings := map[string]string{}
	if c.Core.Deps.Workspace != nil {
		bindings = c.Core.Deps.Workspace.AllBindings()
	}
	selected := make(map[string]Location, len(locations))
	for _, location := range locations {
		current, exists := selected[location.Meta.ID]
		if !exists || preferSessionLocation(location, current, bindings[location.Meta.ID]) {
			selected[location.Meta.ID] = location
		}
	}

	sessions := make([]model.SessionInfo, 0, len(selected))
	discovered := make(map[string]string, len(selected))
	for sessionID, location := range selected {
		meta := location.Meta
		if meta.Name == "" {
			meta.Name = c.sessionName(location, scoped)
		}
		sessions = append(sessions, meta)
		if location.WorkspaceID != "" {
			discovered[sessionID] = location.WorkspaceID
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

func (c *Coordinator) sessionName(location Location, scoped ScopedSessionPort) string {
	key := location.WorkspaceID + "\x00" + location.Meta.ID
	c.sessionNameMu.Lock()
	entry, ok := c.sessionNames[key]
	c.sessionNameMu.Unlock()
	if ok && entry.updatedAt.Equal(location.Meta.UpdatedAt) {
		return entry.name
	}

	name := ""
	if store, ok := c.Core.Deps.Sessions.(SessionRecordPort); ok {
		record, err := store.LoadSessionRecordWorkspace(location.WorkspaceID, location.Meta.ID)
		if err == nil {
			name = strings.TrimSpace(record.Title.Value)
		}
	}
	if name == "" {
		name = c.sessionNameFromTail(location, scoped)
	}
	c.sessionNameMu.Lock()
	c.sessionNames[key] = sessionNameCacheEntry{updatedAt: location.Meta.UpdatedAt, name: name}
	c.sessionNameMu.Unlock()
	return name
}

func (c *Coordinator) sessionNameFromTail(location Location, scoped ScopedSessionPort) string {
	window := c.limits().HistoryWindow
	_, total, err := scoped.LoadHistoryRangeWorkspace(location.WorkspaceID, location.Meta.ID, 0, 0)
	if err != nil {
		return ""
	}
	offset := total - window
	if offset < 0 {
		offset = 0
	}
	history, _, err := scoped.LoadHistoryRangeWorkspace(location.WorkspaceID, location.Meta.ID, offset, window)
	if err != nil {
		return ""
	}
	return SessionTitleFromHistory(history, c.displayUserInput)
}

// InvalidateSessionName 删除会话标题缓存（删除/重命名后立即失效）。
func (c *Coordinator) InvalidateSessionName(sessionID string) {
	suffix := "\x00" + strings.TrimSpace(sessionID)
	c.sessionNameMu.Lock()
	for key := range c.sessionNames {
		if strings.HasSuffix(key, suffix) {
			delete(c.sessionNames, key)
		}
	}
	c.sessionNameMu.Unlock()
}

func (c *Coordinator) clearSessionNames() {
	c.sessionNameMu.Lock()
	clear(c.sessionNames)
	c.sessionNameMu.Unlock()
}

// SessionTitleFromHistory 从历史窗口内的首条可见 user 消息提取标题。
func SessionTitleFromHistory(history []contract.EngineMessage, displayUserInput func(string) string) string {
	for _, message := range history {
		if message.Role != "user" {
			continue
		}
		if title := SessionTitle(displayUserInput(message.Content)); title != "" {
			return title
		}
	}
	return ""
}

// SessionTitle 从输入首行提取会话标题（>48 rune 截断）。
func SessionTitle(input string) string {
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

// ShortSessionID 按 limits.session_name_runes 截断会话 ID 显示。
func (c *Coordinator) ShortSessionID(id string) string {
	runes := []rune(strings.TrimSpace(id))
	maxRunes := c.limits().SessionNameRunes // limits.session_name_runes（默认 16）
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes])
}

func (c *Coordinator) allSessionLocations(scoped ScopedSessionPort) []Location {
	locations := make([]Location, 0)
	for _, meta := range scoped.ListWorkspace("") {
		locations = append(locations, Location{Meta: meta})
	}
	if c.Core.Deps.Workspace == nil {
		return locations
	}
	for _, item := range c.Core.Deps.Workspace.List() {
		workspace := item
		for _, meta := range scoped.ListWorkspace(item.ID) {
			locations = append(locations, Location{WorkspaceID: item.ID, Workspace: &workspace, Meta: meta})
		}
	}
	return locations
}

// LocateSession 定位会话（workspace 绑定优先；支持 scoped 读取时遍历全部
// 工作区，否则退化为当前绑定/默认定位）。
func (c *Coordinator) LocateSession(sessionID string) Location {
	sessionID = strings.TrimSpace(sessionID)
	var boundWorkspace *model.WorkspaceInfo
	if c.Core.Deps.Workspace != nil {
		if workspace, ok := c.Core.Deps.Workspace.SessionWorkspace(sessionID); ok {
			boundWorkspace = &workspace
		}
	}

	scoped, ok := c.Core.Deps.Sessions.(ScopedSessionPort)
	if !ok {
		location := Location{Meta: model.SessionInfo{ID: sessionID}}
		if boundWorkspace != nil {
			location.WorkspaceID = boundWorkspace.ID
			location.Workspace = boundWorkspace
		}
		return location
	}

	var selected Location
	found := false
	for _, location := range c.allSessionLocations(scoped) {
		if location.Meta.ID != sessionID {
			continue
		}
		if !found || preferSessionLocation(location, selected, WorkspaceID(boundWorkspace)) {
			selected = location
			found = true
		}
	}
	if found {
		return selected
	}
	if boundWorkspace != nil {
		return Location{WorkspaceID: boundWorkspace.ID, Workspace: boundWorkspace, Meta: model.SessionInfo{ID: sessionID}}
	}
	return Location{Meta: model.SessionInfo{ID: sessionID}}
}

func preferSessionLocation(candidate, current Location, boundWorkspaceID string) bool {
	if boundWorkspaceID != "" {
		if candidate.WorkspaceID == boundWorkspaceID && current.WorkspaceID != boundWorkspaceID {
			return true
		}
		if current.WorkspaceID == boundWorkspaceID && candidate.WorkspaceID != boundWorkspaceID {
			return false
		}
	}
	return candidate.Meta.UpdatedAt.After(current.Meta.UpdatedAt)
}

// WorkspaceID 返回工作区指针的 ID（nil → ""）。
func WorkspaceID(workspace *model.WorkspaceInfo) string {
	if workspace == nil {
		return ""
	}
	return workspace.ID
}

// LoadSessionHistory 加载会话 provider 历史（scoped 端口优先；回退切换写
// 作用域后读回并恢复）。
func (c *Coordinator) LoadSessionHistory(location Location, sessionID string) ([]contract.EngineMessage, error) {
	if scoped, ok := c.Core.Deps.Sessions.(ScopedSessionPort); ok {
		return scoped.LoadHistoryWorkspace(location.WorkspaceID, sessionID)
	}
	previous := c.Core.Deps.Sessions.Workspace()
	c.Core.Deps.Sessions.SetWorkspace(location.WorkspaceID)
	history, err := c.Core.Deps.Sessions.LoadHistory(sessionID)
	if err != nil {
		c.Core.Deps.Sessions.SetWorkspace(previous)
	}
	return history, err
}

// LoadSessionHistoryRange 按偏移量窗口加载历史（scoped 端口优先）。
func (c *Coordinator) LoadSessionHistoryRange(workspaceID, sessionID string, offset, limit int) ([]contract.EngineMessage, int, error) {
	if scoped, ok := c.Core.Deps.Sessions.(ScopedSessionPort); ok {
		return scoped.LoadHistoryRangeWorkspace(workspaceID, sessionID, offset, limit)
	}
	return c.Core.Deps.Sessions.LoadHistoryRange(sessionID, offset, limit)
}
