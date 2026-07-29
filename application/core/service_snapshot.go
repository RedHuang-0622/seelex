package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (service *Service) Snapshot() Snapshot {
	service.mu.RLock()
	snapshot := cloneSnapshot(service.snapshot)
	service.mu.RUnlock()
	sessions, discoveredBindings := service.sessionCatalog()
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

func (service *Service) Subscribe(buffer int) Subscription { return service.events.Subscribe(buffer) }

func (service *Service) refreshRuntimeLocked(ctx context.Context) {
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

func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	service.messageSeq++
	message := Message{ID: fmt.Sprintf("message-%d", service.messageSeq), Role: role, Content: content, Tool: tool, CreatedAt: time.Now()}
	service.snapshot.Conversation = append(service.snapshot.Conversation, message)
	return &service.snapshot.Conversation[len(service.snapshot.Conversation)-1]
}

func (service *Service) bumpLocked() uint64 {
	service.snapshot.Revision++
	return service.snapshot.Revision
}

func (service *Service) addNotice(notice string) {
	if strings.TrimSpace(notice) == "" {
		return
	}
	service.mu.Lock()
	message := *service.appendMessageLocked("system", notice, nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageAdded, revision, "", message)
}

func (service *Service) resetConversation(notice string) {
	service.mu.Lock()
	service.snapshot.Conversation = nil
	service.snapshot.Session.Name = ""
	service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", service.deps.Runtime.Model()), nil)
	if notice != "" {
		service.appendMessageLocked("system", notice, nil)
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}
