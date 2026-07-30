package core

import (
	"errors"
	"fmt"
	"strings"
)

// resumeSession replaces the active engine history and restores the session's
// workspace binding before publishing one coherent snapshot.
func (service *Service) resumeSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	running := service.snapshot.Chat.Running
	service.mu.RUnlock()
	if running {
		return ErrChatRunning
	}

	location := service.locateSession(sessionID)
	history, err := service.loadSessionHistory(location, sessionID)
	if err != nil {
		return fmt.Errorf("load session %q: %w", sessionID, err)
	}
	if err := service.deps.Engine.ReplaceHistory(sessionID, history); err != nil {
		return fmt.Errorf("replace engine history: %w", err)
	}
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())

	total := len(history)
	offset := total - defaultHistoryWindow
	if offset < 0 {
		offset = 0
	}
	visibleHistory := history[offset:]
	currentWorkspace := location.workspace
	if service.deps.Workspace != nil {
		if currentWorkspace != nil {
			if err := service.deps.Runtime.BindProjectRoot(currentWorkspace.RootPath); err != nil {
				return fmt.Errorf("bind project root: %w", err)
			}
			service.deps.Sessions.SetWorkspace(currentWorkspace.ID)
			service.deps.Workspace.BindSession(sessionID, currentWorkspace.ID)
		} else {
			service.deps.Runtime.UnbindProjectRoot()
			service.deps.Sessions.SetWorkspace("")
			service.deps.Workspace.UnbindSession(sessionID)
		}
	}

	service.mu.Lock()
	service.snapshot.Session = SessionState{ID: sessionID, Name: sessionTitleFromHistory(history)}
	service.snapshot.Conversation = nil
	service.snapshot.Runtime.Plan = nil
	service.snapshot.Interaction = nil
	service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
	service.appendHistoryLocked(visibleHistory)
	service.snapshot.HistoryOffset = offset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = offset > 0
	if service.deps.Workspace != nil {
		service.snapshot.CurrentWorkspace = currentWorkspace
		service.refreshWorkspaceLocked()
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

// LoadMoreHistory prepends an older history page to the visible conversation.
func (service *Service) LoadMoreHistory(limit int) error {
	if limit <= 0 {
		limit = defaultHistoryWindow
	}

	service.mu.RLock()
	offset := service.snapshot.HistoryOffset
	sessionID := service.snapshot.Session.ID
	service.mu.RUnlock()
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	loadLimit := offset - loadOffset

	workspaceID := ""
	service.mu.RLock()
	if service.snapshot.CurrentWorkspace != nil {
		workspaceID = service.snapshot.CurrentWorkspace.ID
	}
	service.mu.RUnlock()
	history, total, err := service.loadSessionHistoryRange(workspaceID, sessionID, loadOffset, loadLimit)
	if err != nil {
		return fmt.Errorf("load history range: %w", err)
	}

	adapted := make([]Message, 0, len(history))
	for _, msg := range history {
		if !isVisibleHistoryMessage(msg) {
			continue
		}
		adapted = append(adapted, adaptEngineMessage(msg))
	}

	service.mu.Lock()
	for index := range adapted {
		service.messageSeq++
		adapted[index].ID = fmt.Sprintf("message-%d", service.messageSeq)
	}
	service.snapshot.Conversation = append(adapted, service.snapshot.Conversation...)
	service.snapshot.HistoryOffset = loadOffset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = loadOffset > 0
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func adaptEngineMessage(msg EngineMessage) Message {
	content := msg.Content
	if msg.Role == "user" {
		content = displayUserInput(content)
	} else if msg.Role == "assistant" || msg.Role == "tool" {
		content = stripThoughtBlocks(content)
	}
	message := Message{Role: msg.Role, Content: content}
	for _, toolCall := range msg.ToolCalls {
		message.Tool = &ToolCall{
			ID: toolCall.ID, Name: toolCall.Name, Arguments: toolCall.Arguments, Status: "success",
		}
	}
	return message
}

func isVisibleHistoryMessage(message EngineMessage) bool {
	if message.Role == "system" {
		return false
	}
	if message.Role != "user" {
		return true
	}
	return displayUserInput(message.Content) != ""
}
