package assembly

import (
	"context"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolhooks "github.com/RedHuang-0622/seelex/application/execution/toolhooks"
	"github.com/RedHuang-0622/seelex/contract"
)

type ToolHookBridge struct {
	bridge *applicationtoolhooks.Bridge
}

// NewToolHookBridge constructs the Seele-to-Application hook adapter.
func NewToolHookBridge() *ToolHookBridge {
	return &ToolHookBridge{bridge: applicationtoolhooks.New()}
}

// Bind installs the current Service as the hook host.
func (bridge *ToolHookBridge) Bind(service *Service) {
	bridge.bridge.Bind(applicationtoolhooks.Deps{
		RecordLLMComplete: service.components.tasks.RecordLLMComplete,
		RecordReActToolCall: func(ctx context.Context) {
			service.components.tasks.RecordReActToolCall(ctx)
		},
		HandleToolStart: func(ctx context.Context, name, id, arguments string) {
			service.components.conversationTurn.projection.Start(ctx, name, id, arguments)
		},
		HandleToolComplete: func(ctx context.Context, info session.ToolCallInfo, id string, observe func(string)) {
			service.components.conversationTurn.projection.Complete(
				ctx, info.Name, id, info.Arguments, info.Result, info.Error, info.Duration, observe,
			)
		},
		AllowNextReActIteration: service.components.tasks.AllowNextReActIteration,
		SessionBacked: func() bool {
			return contract.SessionBacked(service.Deps.Engine)
		},
		QueuedInputs: func() bool {
			service.ViewMu.RLock()
			queued := len(service.activeQueuedChatRequestsLocked()) > 0
			service.ViewMu.RUnlock()
			return queued
		},
		ActiveRequestID: func() string {
			service.ViewMu.RLock()
			requestID := service.Core.Snapshot.Chat.RequestID
			service.ViewMu.RUnlock()
			return requestID
		},
		PrepareHistory: func(ctx context.Context) error {
			return service.components.history.PrepareNewHistoryContentFor(task_context.SessionIDFromContext(ctx))
		},
		RecordContextFailure: service.components.context.RecordContextControlFailure,
	})
}

// SetDiagnosticObserver installs optional metadata-only hook diagnostics.
func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver) {
	bridge.bridge.SetDiagnosticObserver(applicationtoolhooks.DiagnosticObserver(observer))
}

// Hooks returns Seele loop hooks.
func (bridge *ToolHookBridge) Hooks() *session.LoopHooks {
	return bridge.bridge.Hooks()
}

// ToolHookDiagnosticEvent is metadata-only hook instrumentation.
type ToolHookDiagnosticEvent = applicationtoolhooks.DiagnosticEvent

// ToolHookDiagnosticObserver receives best-effort hook stages.
type ToolHookDiagnosticObserver = applicationtoolhooks.DiagnosticObserver
