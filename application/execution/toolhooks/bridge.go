// Package toolhooks adapts Seele loop hooks to application tool projection.
package toolhooks

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/Seele/session"
)

// DiagnosticEvent is metadata-only tool-hook instrumentation.
type DiagnosticEvent struct {
	Stage string
	Name  string
	Err   error
}

// DiagnosticObserver receives best-effort hook stages.
type DiagnosticObserver func(DiagnosticEvent)

// Deps supplies task, queue, history and tool-projection callbacks.
type Deps struct {
	RecordLLMComplete       func(context.Context, session.LLMInfo)
	RecordReActToolCall     func(context.Context)
	HandleToolStart         func(context.Context, string, string, string)
	HandleToolComplete      func(context.Context, session.ToolCallInfo, string, func(string))
	AllowNextReActIteration func(context.Context, int) bool
	SessionBacked           func() bool
	QueuedInputs            func() bool
	ActiveRequestID         func() string
	PrepareHistory          func(context.Context) error
	RecordContextFailure    func(requestID string, err error)
}

// Bridge adapts Seele hooks to application callbacks.
type Bridge struct {
	mu         sync.Mutex
	deps       Deps
	toolSeq    uint64
	pending    map[string][]string
	diagnostic DiagnosticObserver
}

// New constructs a tool-hook bridge.
func New() *Bridge {
	return &Bridge{}
}

// Bind replaces the bridge callbacks.
func (bridge *Bridge) Bind(deps Deps) {
	bridge.mu.Lock()
	bridge.deps = deps
	bridge.mu.Unlock()
}

// SetDiagnosticObserver installs optional hook diagnostics.
func (bridge *Bridge) SetDiagnosticObserver(observer DiagnosticObserver) {
	bridge.mu.Lock()
	bridge.diagnostic = observer
	bridge.mu.Unlock()
}

func (bridge *Bridge) observeDiagnostic(event DiagnosticEvent) {
	bridge.mu.Lock()
	observer := bridge.diagnostic
	bridge.mu.Unlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}

// Hooks returns the Seele loop hooks.
func (bridge *Bridge) Hooks() *session.LoopHooks {
	return &session.LoopHooks{
		OnLLMComplete: func(ctx context.Context, info session.LLMInfo) {
			bridge.mu.Lock()
			record := bridge.deps.RecordLLMComplete
			bridge.mu.Unlock()
			if record != nil {
				record(ctx, info)
			}
		},
		OnToolStart: func(ctx context.Context, info session.ToolCallInfo) {
			bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.start.enter", Name: info.Name})
			deps, id := bridge.beginTool(info)
			bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.start.matched", Name: info.Name})
			if deps.HandleToolStart != nil {
				bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.start.project.start", Name: info.Name})
				if deps.RecordReActToolCall != nil {
					deps.RecordReActToolCall(ctx)
				}
				deps.HandleToolStart(ctx, info.Name, id, info.Arguments)
				bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.start.project.done", Name: info.Name})
			}
		},
		OnToolComplete: func(ctx context.Context, info session.ToolCallInfo) {
			bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.complete.enter", Name: info.Name, Err: info.Error})
			deps, id := bridge.completeTool(info)
			bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.complete.matched", Name: info.Name, Err: info.Error})
			if deps.HandleToolComplete != nil {
				bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.complete.project.start", Name: info.Name, Err: info.Error})
				deps.HandleToolComplete(ctx, info, id, func(stage string) {
					bridge.observeDiagnostic(DiagnosticEvent{Stage: stage, Name: info.Name, Err: info.Error})
				})
				bridge.observeDiagnostic(DiagnosticEvent{Stage: "toolhook.complete.project.done", Name: info.Name, Err: info.Error})
			}
		},
		OnIterationComplete: func(ctx context.Context, turn int) bool {
			bridge.mu.Lock()
			deps := bridge.deps
			bridge.mu.Unlock()
			if deps.AllowNextReActIteration == nil {
				return true
			}
			if !deps.AllowNextReActIteration(ctx, turn) {
				return false
			}
			if deps.SessionBacked != nil && deps.SessionBacked() {
				return !(deps.QueuedInputs != nil && deps.QueuedInputs())
			}
			requestID := ""
			if deps.ActiveRequestID != nil {
				requestID = deps.ActiveRequestID()
			}
			if deps.PrepareHistory != nil {
				if err := deps.PrepareHistory(ctx); err != nil {
					if deps.RecordContextFailure != nil {
						deps.RecordContextFailure(requestID, err)
					}
					return false
				}
			}
			return true
		},
	}
}

func (bridge *Bridge) beginTool(info session.ToolCallInfo) (Deps, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	id := bridge.nextToolIDLocked()
	if bridge.pending == nil {
		bridge.pending = make(map[string][]string)
	}
	key := toolKey(info)
	bridge.pending[key] = append(bridge.pending[key], id)
	return bridge.deps, id
}

func (bridge *Bridge) completeTool(info session.ToolCallInfo) (Deps, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	key := toolKey(info)
	ids := bridge.pending[key]
	if len(ids) == 0 {
		return bridge.deps, bridge.nextToolIDLocked()
	}
	id := ids[0]
	if len(ids) == 1 {
		delete(bridge.pending, key)
	} else {
		bridge.pending[key] = ids[1:]
	}
	return bridge.deps, id
}

func (bridge *Bridge) nextToolIDLocked() string {
	bridge.toolSeq++
	return fmt.Sprintf("tool-%d", bridge.toolSeq)
}

func toolKey(info session.ToolCallInfo) string {
	return fmt.Sprintf("%d\x00%s\x00%s", info.Turn, info.Name, info.Arguments)
}
