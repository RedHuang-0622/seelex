package session_runtime

import (
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
)

// sessionCatalogProject 枚举单个项目的会话集合（G6：目录按 projectID 分格，
// 项目 = 会话集合；worker 逐项目刷新格子）。返回该项目会话行与该轮发现的工作
// 区绑定（projectID != "" 的会话即归属该项目）。标题从 record 读取（不再走
// workspace 粒度 tail 回退）。
func (c *Coordinator) sessionCatalogProject(granular SessionGranularPort, projectID string) ([]model.SessionInfo, map[string]string) {
	discovered := map[string]string{}
	sessions := []model.SessionInfo{}
	for _, info := range granular.SessionsOf(projectID) {
		// C2 归档过滤：archived 是 record 级粘性状态，归档会话不进常规目录
		// 行（按项目分格过滤，避免在别的项目格/全局数组上留标记位）。存储层
		// 枚举仍返回归档行——按 ID 冷读/重开/定位不受影响。
		if info.Status == model.SessionStatusArchived {
			continue
		}
		if projectID != "" {
			discovered[info.ID] = projectID
		}
		if info.Name == "" {
			if record, ok, err := c.loadGranularRecord(info.ID); err == nil && ok && record.Title.Value != "" {
				info.Name = record.Title.Value
			} else if name := c.sessionNameFromTail(granular, info.ID); name != "" {
				info.Name = name
			}
		}
		sessions = append(sessions, info)
	}
	return sessions, discovered
}

// sessionNameFromTail 从会话历史尾部窗口提取标题（会话粒度端口；
// record 标题缺失时的回退）。
func (c *Coordinator) sessionNameFromTail(granular SessionGranularPort, sessionID string) string {
	window := c.limits().HistoryWindow
	_, total, err := granular.LoadHistoryRange(sessionID, 0, 0)
	if err != nil {
		return ""
	}
	offset := total - window
	if offset < 0 {
		offset = 0
	}
	history, _, err := granular.LoadHistoryRange(sessionID, offset, window)
	if err != nil {
		return ""
	}
	return SessionTitleFromHistory(history, c.displayUserInput)
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

// allProjectIDs 返回目录枚举的项目集合（空项目 = 当前 active scope + 全部
// workspace 绑定项目；去重）。
func (c *Coordinator) allProjectIDs() []string {
	seen := map[string]bool{"": true}
	projects := []string{""}
	if c.Core.Deps.Workspace == nil {
		return projects
	}
	for _, item := range c.Core.Deps.Workspace.List() {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		projects = append(projects, item.ID)
	}
	return projects
}

// AllProjectIDs 返回目录枚举的项目集合（公开观察面：冷启动草稿恢复需要跨
// 项目找 record，与 worker 的枚举范围一致）。
func (c *Coordinator) AllProjectIDs() []string {
	return c.allProjectIDs()
}

// DraftCandidates 返回目录里 status=draft 的会话候选（G：跨项目枚举，用于
// 冷启动装配器恢复草稿——工作区草稿按绑定项目落 record 后在此找回）。
func (c *Coordinator) DraftCandidates() []model.SessionInfo {
	var candidates []model.SessionInfo
	if granular, ok := c.Core.Deps.Sessions.(SessionGranularPort); ok {
		for _, projectID := range c.allProjectIDs() {
			for _, info := range granular.SessionsOf(projectID) {
				if info.Status == model.SessionStatusDraft && info.ID != "" {
					candidates = append(candidates, info)
				}
			}
		}
	} else {
		for _, info := range c.Core.Deps.Sessions.List() {
			if info.Status == model.SessionStatusDraft && info.ID != "" {
				candidates = append(candidates, info)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})
	return candidates
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

	granular, ok := c.Core.Deps.Sessions.(SessionGranularPort)
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
	for _, projectID := range c.allProjectIDs() {
		for _, info := range granular.SessionsOf(projectID) {
			if info.ID != sessionID {
				continue
			}
			location := Location{Meta: info}
			if projectID != "" {
				if workspace, err := c.Core.Deps.Workspace.Get(projectID); err == nil {
					location.WorkspaceID = projectID
					location.Workspace = &workspace
				}
			}
			if !found || preferSessionLocation(location, selected, WorkspaceID(boundWorkspace)) {
				selected = location
				found = true
			}
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

func preferSessionInfo(candidate, current model.SessionInfo) bool {
	return candidate.UpdatedAt.After(current.UpdatedAt)
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
	if granular, ok := c.Core.Deps.Sessions.(SessionGranularPort); ok {
		return granular.LoadHistory(sessionID)
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
	if granular, ok := c.Core.Deps.Sessions.(SessionGranularPort); ok {
		return granular.LoadHistoryRange(sessionID, offset, limit)
	}
	return c.Core.Deps.Sessions.LoadHistoryRange(sessionID, offset, limit)
}

// loadGranularRecord 读取会话 record（会话粒度；不存在返回 false）。
func (c *Coordinator) loadGranularRecord(sessionID string) (model.SessionRecord, bool, error) {
	if store, ok := c.Core.Deps.Sessions.(SessionRecordPort); ok {
		record, err := store.LoadSessionRecord(sessionID)
		if err != nil {
			return model.SessionRecord{}, false, err
		}
		if record.Version != SessionRecordVersion || record.ID != sessionID {
			return model.SessionRecord{}, false, nil
		}
		return record, true, nil
	}
	return model.SessionRecord{}, false, nil
}
