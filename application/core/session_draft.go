package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const draftSessionName = "新会话"

// BeginNewSession enters an idempotent, unpersisted draft state. The engine
// session is created only when the first real conversation request is sent.
func (service *Service) BeginNewSession() error {
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	closed := service.closed
	draining := service.draining
	running := service.snapshot.Chat.Running
	draft := service.snapshot.Session.Draft
	sessionID := service.snapshot.Session.ID
	currentWorkspaceID := workspaceID(service.snapshot.CurrentWorkspace)
	service.mu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	if running {
		return ErrChatRunning
	}
	if draft {
		return nil
	}

	if len(service.deps.Engine.History()) > 0 {
		service.deps.Sessions.SetWorkspace(currentWorkspaceID)
		if err := service.persistCurrentSession(sessionID); err != nil {
			return fmt.Errorf("save current session before drafting a new one: %w", err)
		}
	}
	service.deps.Engine.ClearHistory()

	service.mu.Lock()
	service.snapshot.Session = SessionState{Name: draftSessionName, Draft: true}
	service.snapshot.Conversation = nil
	service.snapshot.Chat = ChatState{}
	service.snapshot.HistoryOffset = 0
	service.snapshot.TotalMessages = 0
	service.snapshot.HasMoreHistory = false
	service.snapshot.Runtime.Plan = nil
	service.snapshot.Interaction = nil
	service.sessionTitle = SessionTitle{}
	service.planStack = nil
	service.activePlanID = ""
	service.planSequence = 0
	service.inputQueue = nil
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

// materializeDraftSession creates the engine session and project binding for
// the first request. The caller must hold sessionTransitionMu.
func (service *Service) materializeDraftSession(firstQuestion string) error {
	service.mu.RLock()
	draft := service.snapshot.Session.Draft
	var workspace *WorkspaceInfo
	if service.snapshot.CurrentWorkspace != nil {
		item := *service.snapshot.CurrentWorkspace
		workspace = &item
	}
	service.mu.RUnlock()
	if !draft {
		return nil
	}

	if workspace != nil {
		if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
			return fmt.Errorf("bind project root for new session: %w", err)
		}
		service.deps.Sessions.SetWorkspace(workspace.ID)
	} else {
		service.deps.Runtime.UnbindProjectRoot()
		service.deps.Sessions.SetWorkspace("")
	}
	newID := strings.TrimSpace(service.deps.Engine.StartSession())
	if newID == "" {
		return errors.New("engine returned an empty session ID")
	}
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
	if workspace != nil && service.deps.Workspace != nil {
		service.deps.Workspace.BindSession(newID, workspace.ID)
	}

	service.mu.Lock()
	title := SessionTitle{Value: sessionTitle(firstQuestion), Source: "first_request", FinalizedAt: time.Now()}
	service.snapshot.Session = SessionState{ID: newID, Name: title.Value}
	service.sessionTitle = title
	service.planStack = nil
	service.activePlanID = ""
	service.planSequence = 0
	if service.deps.Workspace != nil {
		service.refreshWorkspaceLocked()
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}
