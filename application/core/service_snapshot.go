package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// viewCoordinator owns user-visible snapshots, messages, and event revisions.
type viewCoordinator struct {
	*serviceState
	sessions *sessionCoordinator
}

func newViewCoordinator(state *serviceState, sessions *sessionCoordinator) *viewCoordinator {
	return &viewCoordinator{serviceState: state, sessions: sessions}
}

func (service *viewCoordinator) snapshotView() Snapshot {
	service.mu.RLock()
	snapshot := cloneSnapshot(service.snapshot)
	service.mu.RUnlock()
	sessions, discoveredBindings := service.sessions.sessionCatalog()
	snapshot.Sessions = sessions
	if snapshot.Session.Name == "" {
		for _, session := range sessions {
			if session.ID == snapshot.Session.ID {
				snapshot.Session.Name = session.Name
				break
			}
		}
	}
	if len(discoveredBindings) > 0 {
		if snapshot.SessionWorkspaces == nil {
			snapshot.SessionWorkspaces = make(map[string]string, len(discoveredBindings))
		}
		for sessionID, workspaceID := range discoveredBindings {
			snapshot.SessionWorkspaces[sessionID] = workspaceID
		}
	}
	return snapshot
}

func (service *viewCoordinator) subscribe(buffer int) Subscription {
	return service.events.Subscribe(buffer)
}

func (service *viewCoordinator) refreshRuntimeLocked(ctx context.Context) {
	service.snapshot.Session.ID = service.deps.Engine.SessionID()
	service.snapshot.Runtime.Model = service.deps.Runtime.Model()
	service.snapshot.Runtime.Provider = service.deps.Runtime.Provider()
	service.snapshot.Runtime.Plugin = service.deps.Runtime.ActivePlugin()
	service.snapshot.Runtime.Effort = service.effortManager.Current()
	service.snapshot.Runtime.VisibleTools = append([]Tool(nil), service.deps.Runtime.VisibleTools(ctx)...)
	service.snapshot.Runtime.Skills = append([]SkillInfo(nil), service.deps.Skills.All()...)
	service.snapshot.Runtime.Tokens = service.deps.Engine.TokenCount()
	metrics := service.deps.Runtime.ReplanMetrics()
	service.snapshot.Runtime.Replan = ReplanMonitor{
		InFlight: metrics.InFlight, ConcurrentLimit: metrics.ConcurrentLimit,
		WindowAttempts: metrics.WindowAttempts, WindowLimit: metrics.WindowLimit,
		WindowStartedAt: metrics.WindowStartedAt, Accepted: metrics.Accepted,
		Succeeded: metrics.Succeeded, Failed: metrics.Failed, Rejected: metrics.Rejected,
		DuplicateRejected: metrics.DuplicateRejected, ProviderRequests: metrics.ProviderRequests,
		ProviderWindowRequests: metrics.ProviderWindowRequests, ProviderWindowLimit: metrics.ProviderWindowLimit,
	}
	service.snapshot.Runtime.Plugins = append([]PluginInfo(nil), service.deps.Plugins.All()...)
	service.snapshot.Runtime.Accounts = append([]AccountInfo(nil), service.deps.Runtime.Accounts()...)
}

func (service *viewCoordinator) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	if role == "assistant" || role == "tool_result" {
		content = stripThoughtBlocks(content)
	}
	service.messageSeq++
	message := Message{ID: fmt.Sprintf("message-%d", service.messageSeq), Role: role, Content: content, Tool: tool, CreatedAt: time.Now()}
	service.snapshot.Conversation = append(service.snapshot.Conversation, message)
	return &service.snapshot.Conversation[len(service.snapshot.Conversation)-1]
}

func (service *viewCoordinator) bumpLocked() uint64 {
	service.snapshot.Revision++
	return service.snapshot.Revision
}

func (service *viewCoordinator) addNotice(notice string) {
	if strings.TrimSpace(notice) == "" {
		return
	}
	service.mu.Lock()
	message := *service.appendMessageLocked("system", notice, nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageAdded, revision, "", message)
}

func (service *viewCoordinator) resetConversation(notice string) {
	service.mu.Lock()
	service.snapshot.Conversation = nil
	service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", service.deps.Runtime.Model()), nil)
	if notice != "" {
		service.appendMessageLocked("system", notice, nil)
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}

func (service *Service) Snapshot() Snapshot {
	return service.components.view.snapshotView()
}

func (service *Service) Subscribe(buffer int) Subscription {
	return service.components.view.subscribe(buffer)
}

func (service *Service) refreshRuntimeLocked(ctx context.Context) {
	service.components.view.refreshRuntimeLocked(ctx)
}

func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	return service.components.view.appendMessageLocked(role, content, tool)
}

func (service *Service) bumpLocked() uint64 {
	return service.components.view.bumpLocked()
}

func (service *Service) addNotice(notice string) {
	service.components.view.addNotice(notice)
}

func (service *Service) resetConversation(notice string) {
	service.components.view.resetConversation(notice)
}
