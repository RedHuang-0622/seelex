package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
)

func (service *Service) startChat(parent context.Context, input string) error {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.snapshot.Chat.Running {
		service.mu.Unlock()
		return ErrChatRunning
	}
	requestID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
	chatContext, cancel := context.WithCancel(parent)
	service.cancelChat = cancel
	service.snapshot.Chat = ChatState{Running: true, RequestID: requestID, StartedAt: time.Now()}
	user := *service.appendMessageLocked("user", input, nil)
	assistant := *service.appendMessageLocked("assistant", "", nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageAdded, revision, requestID, user)
	service.events.Publish(EventMessageAdded, revision, requestID, assistant)
	go service.runChat(chatContext, requestID, input)
	return nil
}

func (service *Service) runChat(ctx context.Context, requestID, input string) {
	_, err := service.deps.Engine.ChatStream(ctx, input, func(chunk string) { service.appendDelta(requestID, chunk) })
	service.mu.Lock()
	if service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	service.snapshot.Chat.Running = false
	service.snapshot.Chat.Error = ""
	service.cancelChat = nil
	if err != nil {
		service.snapshot.Chat.Error = err.Error()
		service.appendMessageLocked("error", err.Error(), nil)
	} else {
		service.snapshot.Conversation = nil
		service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", service.deps.Runtime.Model()), nil)
		service.appendHistoryLocked(service.deps.Engine.History())
	}
	service.refreshRuntimeLocked(context.Background())
	revision := service.bumpLocked()
	service.mu.Unlock()
	if err != nil {
		service.events.Publish(EventError, revision, requestID, map[string]string{"message": err.Error()})
	} else {
		service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	}
}

func (service *Service) appendDelta(requestID, chunk string) {
	service.mu.Lock()
	if !service.snapshot.Chat.Running || service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		if service.snapshot.Conversation[index].Role == "assistant" && service.snapshot.Conversation[index].Tool == nil {
			service.snapshot.Conversation[index].Content += chunk
			break
		}
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageDelta, revision, requestID, map[string]string{"delta": chunk})
}

func (service *Service) appendHistoryLocked(history []EngineMessage) {
	for _, historyMessage := range history {
		if historyMessage.Role != "tool" && historyMessage.Content != "" {
			service.appendMessageLocked(historyMessage.Role, historyMessage.Content, nil)
		}
		for _, call := range historyMessage.ToolCalls {
			service.appendMessageLocked("tool", "", &ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"})
		}
		if historyMessage.Role == "tool" {
			service.appendMessageLocked("tool_result", historyMessage.Content, &ToolCall{Name: historyMessage.Name, Result: historyMessage.Content, Status: "success"})
		}
	}
}

func (service *Service) handleToolStart(name, id, arguments string) {
	service.mu.Lock()
	tool := &ToolCall{ID: id, Name: name, Arguments: arguments, Status: "running"}
	message := *service.appendMessageLocked("tool", "", tool)
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	service.events.Publish(EventToolStarted, revision, requestID, message)
}

func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration) {
	service.mu.Lock()
	status, errorText := "success", ""
	if toolErr != nil {
		status, errorText = "error", toolErr.Error()
	}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		tool := service.snapshot.Conversation[index].Tool
		if tool != nil && tool.ID == id {
			tool.Status, tool.Result, tool.Error, tool.Duration = status, result, errorText, duration
			break
		}
	}
	content := result
	if toolErr != nil {
		content = errorText
	}
	message := *service.appendMessageLocked("tool_result", content, &ToolCall{ID: id, Name: name, Result: result, Error: errorText, Status: status, Duration: duration})
	service.appendMessageLocked("assistant", "", nil)
	service.refreshRuntimeLocked(context.Background())
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	service.events.Publish(EventToolCompleted, revision, requestID, message)
}

type ToolHookBridge struct {
	mu      sync.RWMutex
	service *Service
}

func NewToolHookBridge() *ToolHookBridge { return &ToolHookBridge{} }
func (bridge *ToolHookBridge) Bind(service *Service) {
	bridge.mu.Lock()
	bridge.service = service
	bridge.mu.Unlock()
}
func (bridge *ToolHookBridge) Hooks() *engine.LoopHooks {
	return &engine.LoopHooks{
		OnToolStart: func(_ context.Context, info engine.ToolCallInfo) {
			bridge.mu.RLock()
			service := bridge.service
			bridge.mu.RUnlock()
			if service != nil {
				service.handleToolStart(info.Name, fmt.Sprintf("%s-%d", info.Name, info.Turn), info.Arguments)
			}
		},
		OnToolComplete: func(_ context.Context, info engine.ToolCallInfo) {
			bridge.mu.RLock()
			service := bridge.service
			bridge.mu.RUnlock()
			if service != nil {
				service.handleToolComplete(info.Name, fmt.Sprintf("%s-%d", info.Name, info.Turn), info.Result, info.Error, info.Duration)
			}
		},
	}
}
