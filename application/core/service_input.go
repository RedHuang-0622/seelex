package core

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/Seele/types"
)

// subagentContextMarker 标记子代理产出块（模型不误读为普通用户轮次）。
const subagentContextMarker = "[子代理产出] "

// injectPendingSubagentContexts drains the Runtime-owned bounded mailbox
// (单一来源 = Runtime mailbox；无本地兼容队列), then injects messages into
// Engine outside service.mu.
// Snapshot mutation is a separate short critical section, so Engine cannot
// form a reverse wait cycle with Application.
func (service *Service) injectPendingSubagentContexts() {
	pending := service.deps.Runtime.DrainSubagentContexts()
	if len(pending) == 0 {
		return
	}

	for _, content := range pending {
		value := subagentContextMarker + content
		service.deps.Engine.AppendHistory(types.Message{Role: "user", Content: &value})
	}
}

func (service *Service) Submit(ctx context.Context, text string) error {
	service.mu.RLock()
	draining := service.draining
	closed := service.closed
	service.mu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	input := strings.TrimSpace(text)
	if input == "" {
		return nil
	}
	return service.components.input.Dispatch(ctx, input)
}

func (service *Service) submitConversation(ctx context.Context, input string) error {
	request := newChatRequest(input, service.promptStack.Layers())
	effort := service.effortManager.Current()
	request.budget = reactBudgetFor(effort)
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()
	if err := service.materializeDraftSession(request.displayInput); err != nil {
		return err
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return errors.New("application is shut down")
	}
	if service.draining {
		service.mu.Unlock()
		return ErrApplicationDraining
	}
	if service.snapshot.Chat.Running {
		service.inputQueue = append(service.inputQueue, request)
		service.snapshot.Chat.InputQueue = chatRequestDisplays(service.inputQueue)
		service.snapshot.Chat.QueuedCount = len(service.inputQueue)
		revision := service.bumpLocked()
		service.mu.Unlock()
		service.events.Publish(EventSnapshotChanged, revision, "", nil)
		return nil
	}
	service.mu.Unlock()
	return service.startChat(ctx, request)
}

// BeginGracefulShutdown stops new user input while allowing the active chat
// and any input already queued behind it to finish naturally.
func (service *Service) BeginGracefulShutdown() {
	service.mu.Lock()
	service.draining = true
	service.mu.Unlock()
}

// WaitForIdle waits for all accepted chat work to finish. It never cancels an
// active chat; callers control abandonment through ctx.
func (service *Service) WaitForIdle(ctx context.Context) error {
	for {
		service.mu.RLock()
		idle := service.idle
		service.mu.RUnlock()
		select {
		case <-idle:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (service *Service) CancelChat(requestID string) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !service.snapshot.Chat.Running || (requestID != "" && requestID != service.snapshot.Chat.RequestID) || service.cancelChat == nil {
		return false
	}
	service.cancelChat()
	return true
}

func (service *Service) Shutdown() {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return
	}
	service.closed = true
	if service.cancelChat != nil {
		service.cancelChat()
	}
	service.mu.Unlock()
	service.stopSessionCatalogRefresh()
	service.stopLifecycleConsumers()
	if service.workTablePublisher != nil {
		service.workTablePublisher.Close()
	}
	service.approval.Shutdown()
}
