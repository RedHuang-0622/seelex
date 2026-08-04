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
	return snapshot
}

func (service *viewCoordinator) subscribe(buffer int) Subscription {
	return service.events.Subscribe(buffer)
}

type runtimeStateProjection struct {
	sessionID string
	runtime   RuntimeState
}

// collectRuntimeProjection calls external ports without service.mu. Applying
// the returned immutable value is a separate in-memory state transition.
func (service *viewCoordinator) collectRuntimeProjection(ctx context.Context) runtimeStateProjection {
	projection := runtimeStateProjection{
		sessionID: service.deps.Engine.SessionID(),
		runtime: RuntimeState{
			Model:        service.deps.Runtime.Model(),
			Provider:     service.deps.Runtime.Provider(),
			Plugin:       service.deps.Runtime.ActivePlugin(),
			Effort:       service.effortManager.Current(),
			FullAccess:   service.deps.Runtime.FullAccess(),
			VisibleTools: append([]Tool(nil), service.deps.Runtime.VisibleTools(ctx)...),
			Skills:       append([]SkillInfo(nil), service.deps.Skills.All()...),
			Tokens:       service.deps.Engine.TokenCount(),
			Plugins:      append([]PluginInfo(nil), service.deps.Plugins.All()...),
			Accounts:     append([]AccountInfo(nil), service.deps.Runtime.Accounts()...),
		},
	}
	metrics := service.deps.Runtime.ReplanMetrics()
	projection.runtime.Replan = ReplanMonitor{
		InFlight: metrics.InFlight, ConcurrentLimit: metrics.ConcurrentLimit,
		WindowAttempts: metrics.WindowAttempts, WindowLimit: metrics.WindowLimit,
		WindowStartedAt: metrics.WindowStartedAt, Accepted: metrics.Accepted,
		Succeeded: metrics.Succeeded, Failed: metrics.Failed, Rejected: metrics.Rejected,
		DuplicateRejected: metrics.DuplicateRejected, ProviderRequests: metrics.ProviderRequests,
		ProviderWindowRequests: metrics.ProviderWindowRequests, ProviderWindowLimit: metrics.ProviderWindowLimit,
	}
	return projection
}

func (service *viewCoordinator) applyRuntimeProjectionLocked(projection runtimeStateProjection) {
	plan := service.snapshot.Runtime.Plan
	account := service.snapshot.Runtime.Account
	service.snapshot.Session.ID = projection.sessionID
	service.snapshot.Runtime = projection.runtime
	service.snapshot.Runtime.Plan = plan
	service.snapshot.Runtime.Account = account
}

func (service *viewCoordinator) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	if role == "assistant" || role == "tool_result" {
		content = stripThoughtBlocks(content)
	}
	service.messageSeq++
	message := Message{ID: fmt.Sprintf("message-%d", service.messageSeq), Role: role, Content: content, Tool: tool, CreatedAt: time.Now()}
	visibleBefore := durableConversationCount(service.snapshot.Conversation)
	service.snapshot.Conversation = append(service.snapshot.Conversation, message)
	if role != "system" {
		if service.snapshot.TotalMessages < visibleBefore {
			service.snapshot.TotalMessages = visibleBefore
		}
		service.snapshot.TotalMessages++
	}
	service.boundConversationTailLocked()
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		if service.snapshot.Conversation[index].ID == message.ID {
			return &service.snapshot.Conversation[index]
		}
	}
	return &message
}

func (service *viewCoordinator) boundConversationTailLocked() {
	window := Limits().HistoryWindow
	if window <= 0 {
		window = 1
	}
	service.snapshot.ConversationWindow = window
	service.snapshot.Conversation = boundConversationTail(service.snapshot.Conversation, window)
	visible := durableConversationCount(service.snapshot.Conversation)
	service.snapshot.HistoryOffset = service.snapshot.TotalMessages - visible
	if service.snapshot.HistoryOffset < 0 {
		service.snapshot.HistoryOffset = 0
	}
	service.snapshot.HasMoreHistory = service.snapshot.HistoryOffset > 0
}

func durableConversationCount(messages []Message) int {
	count := 0
	for _, message := range messages {
		if message.Role != "system" {
			count++
		}
	}
	return count
}

func boundConversationTail(messages []Message, window int) []Message {
	if window <= 0 {
		return nil
	}
	keep := make([]bool, len(messages))
	nonSystem, system := 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "system" {
			if system < window {
				keep[index] = true
				system++
			}
			continue
		}
		if nonSystem < window {
			keep[index] = true
			nonSystem++
		}
	}
	bounded := make([]Message, 0, nonSystem+system)
	for index, message := range messages {
		if keep[index] {
			bounded = append(bounded, message)
		}
	}
	return bounded
}

func boundConversationHead(messages []Message, window int) []Message {
	if window <= 0 {
		return nil
	}
	bounded := make([]Message, 0, min(len(messages), window*2))
	nonSystem, system := 0, 0
	for _, message := range messages {
		if message.Role == "system" {
			if system < window {
				bounded = append(bounded, message)
				system++
			}
			continue
		}
		if nonSystem < window {
			bounded = append(bounded, message)
			nonSystem++
		}
	}
	return bounded
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
	model := service.deps.Runtime.Model()
	service.mu.Lock()
	service.snapshot.Conversation = nil
	service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", model), nil)
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

func (service *Service) collectRuntimeProjection(ctx context.Context) runtimeStateProjection {
	return service.components.view.collectRuntimeProjection(ctx)
}

func (service *Service) applyRuntimeProjectionLocked(projection runtimeStateProjection) {
	service.components.view.applyRuntimeProjectionLocked(projection)
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
