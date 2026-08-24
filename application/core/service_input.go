package core

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/core/view_state"
)

// injectPendingSubagentContexts 排空 Runtime 持有的有界邮箱（单一来源 =
// Runtime mailbox；无本地兼容队列），并在 service.Mu 之外把消息注入 Engine。
// 快照变更是独立短临界区，Engine 不会与 Application 形成反向等待环。
func (service *Service) injectPendingSubagentContexts() {
	pending := service.Deps.Runtime.DrainSubagentContexts()
	if len(pending) == 0 {
		return
	}

	for _, content := range pending {
		value := view_state.SubagentContextMarker + content
		service.Deps.Engine.AppendHistory(types.Message{Role: "user", Content: &value})
	}
}

func (service *Service) Submit(ctx context.Context, text string) error {
	service.Mu.RLock()
	draining := service.draining
	closed := service.closed
	service.Mu.RUnlock()
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
	transition := service.components.sessions.TransitionLock()
	transition.Lock()
	defer transition.Unlock()
	if err := service.materializeDraftSession(request.displayInput); err != nil {
		return err
	}
	service.Mu.Lock()
	if service.closed {
		service.Mu.Unlock()
		return errors.New("application is shut down")
	}
	if service.draining {
		service.Mu.Unlock()
		return ErrApplicationDraining
	}
	sessionID := service.Core.Snapshot.Session.ID
	runtime := service.sessionChatLocked(sessionID)
	if runtime.chat.Running {
		runtime.inputQueue = append(runtime.inputQueue, request)
		service.inputQueue = runtime.inputQueue
		runtime.chat.InputQueue = chatRequestDisplays(runtime.inputQueue)
		runtime.chat.QueuedCount = len(runtime.inputQueue)
		service.Core.Snapshot.Chat = runtime.chat
		revision := service.bumpLocked()
		service.Mu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		return nil
	}
	service.Mu.Unlock()
	return service.startChat(ctx, request)
}

// BeginGracefulShutdown 停止接收新输入，同时允许活跃 chat 及其已排队输入
// 自然完成。
func (service *Service) BeginGracefulShutdown() {
	service.Mu.Lock()
	service.draining = true
	service.Mu.Unlock()
}

// WaitForIdle 等待全部已接受的 chat 工作完成。它从不取消活跃 chat；调用方
// 通过 ctx 控制放弃。
func (service *Service) WaitForIdle(ctx context.Context) error {
	for {
		service.Mu.RLock()
		idle := service.idle
		service.Mu.RUnlock()
		select {
		case <-idle:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (service *Service) CancelChat(requestID string) bool {
	service.Mu.Lock()
	defer service.Mu.Unlock()
	sessionID := service.Core.Snapshot.Session.ID
	runtime := service.sessionChatLocked(sessionID)
	if !runtime.chat.Running || (requestID != "" && requestID != runtime.chat.RequestID) || runtime.cancel == nil {
		return false
	}
	runtime.cancel()
	return true
}

func (service *Service) Shutdown() {
	service.Mu.Lock()
	if service.closed {
		service.Mu.Unlock()
		return
	}
	service.closed = true
	if service.cancelChat != nil {
		service.cancelChat()
	}
	service.Mu.Unlock()
	service.components.sessions.StopCatalogRefresh()
	service.stopLifecycleConsumers()
	if service.workTablePublisher != nil {
		service.workTablePublisher.Close()
	}
	service.Approval.Shutdown()
}
