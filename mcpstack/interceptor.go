package mcpstack

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ── MCPCallRecorder ─────────────────────────────────────────────
//
// The Interceptor is the key middleware component that wraps every MCP call.
// It implements Stack 2 of the dual-stack architecture:
//
//   Stack 1 — MCPStack: the immutable call history (data layer)
//   Stack 2 — Interceptor: hooks into the call lifecycle (middleware layer)
//
// Usage pattern:
//
//	// Before MCP call
//	rec := mcpstack.BeforeCall("freecad", "sketch_rectangle", argsJSON)
//
//	// Execute actual MCP call
//	result, err := server.Call(ctx, toolName, args)
//
//	// After MCP call
//	rec.AfterCall(resultJSON, err)

// CallRecorder handles a single MCP call's lifecycle: before, after, and rollback.
type CallRecorder struct {
	stack    *MCPStack
	call     MCPCall
	recorded bool // true if the initial Record() succeeded
}

// Call returns the underlying MCPCall data (for middleware that needs to inspect it).
func (r *CallRecorder) Call() MCPCall { return r.call }

// BeforeCall creates a new CallRecorder and records a "pending" entry in the stack.
// Call this BEFORE dispatching to the actual MCP server.
//
// Parameters:
//   - serverName: name of the target MCP server (e.g. "freecad", "chem-sim")
//   - toolName:   MCP tool name (e.g. "sketch_rectangle", "simulate_binding")
//   - argsJSON:   raw JSON arguments (already serialized)
//   - aiBacklink: optional AI message ID that triggered this call
//
// Returns a CallRecorder that must be completed via AfterCall().
func BeforeCall(stack *MCPStack, serverName, toolName string, argsJSON json.RawMessage, aiBacklink string) *CallRecorder {
	call := MCPCall{
		ID:         uuid.New().String(),
		Timestamp:  time.Now().UTC(),
		ServerName: serverName,
		ToolName:   toolName,
		Args:       argsJSON,
		Status:     StatusPending,
		AIBacklink: aiBacklink,
		TokenCount: estimateTokenCount(serverName, toolName, argsJSON),
	}

	// Best-effort record (don't block the call on persistence errors)
	rec := &CallRecorder{stack: stack, call: call}
	if err := stack.Record(call); err == nil {
		rec.recorded = true
	}

	return rec
}

// AfterCall completes the recorded call with its result or error.
// Call this AFTER the MCP server responds (whether success or failure).
func (r *CallRecorder) AfterCall(resultJSON json.RawMessage, callErr error) {
	if !r.recorded {
		return // Wasn't recorded (initial record failed)
	}

	r.call.Result = resultJSON
	r.call.ErrorMsg = ""
	if callErr != nil {
		r.call.Status = StatusFailed
		r.call.ErrorMsg = callErr.Error()
	} else {
		r.call.Status = StatusSuccess
	}
	r.call.Timestamp = time.Now().UTC()

	// Update the call in the stack
	r.stack.mu.Lock()
	defer r.stack.mu.Unlock()

	for i := len(r.stack.Calls) - 1; i >= 0; i-- {
		if r.stack.Calls[i].ID == r.call.ID {
			r.stack.Calls[i].Status = r.call.Status
			r.stack.Calls[i].Result = r.call.Result
			r.stack.Calls[i].ErrorMsg = r.call.ErrorMsg
			r.stack.Calls[i].Timestamp = r.call.Timestamp
			break
		}
	}
	r.stack.UpdatedAt = time.Now().UTC()
	_ = r.stack.autoSave()
}

// ── Middleware function type ─────────────────────────────────────

// Callback is the signature for a function that can wrap MCP calls.
// It receives the context, recorder, and a "next" function that actually
// dispatches the call. This enables layered middleware (logging, metrics, auth, …).
type Callback func(ctx context.Context, rec *CallRecorder, next func(ctx context.Context) (json.RawMessage, error)) (json.RawMessage, error)

// ── Helpers ─────────────────────────────────────────────────────

func estimateTokenCount(serverName, toolName string, args json.RawMessage) int {
	tokens := len(serverName)/2 + len(toolName)/2 + len(args)/3 + 10
	if tokens < 20 {
		tokens = 20
	}
	return tokens
}

// ── FormatSummary ───────────────────────────────────────────────

// FormatSummary returns a dense one-line summary of all active calls grouped by server.
func FormatSummary(s *MCPStack) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	active := s.CurrentIdx + 1
	total := len(s.Calls)
	if active == 0 {
		return "MCP 调用栈为空"
	}

	serverGroups := make(map[string]int)
	statusCounts := map[CallStatus]int{
		StatusSuccess:   0,
		StatusFailed:    0,
		StatusPending:   0,
		StatusRolledBack: 0,
	}

	for _, call := range s.Calls[:active] {
		serverGroups[call.ServerName]++
		statusCounts[call.Status]++
	}

	servers := ""
	for name, count := range serverGroups {
		servers += fmt.Sprintf("%s:%d ", name, count)
	}

	return fmt.Sprintf("MCP trace: %d calls, %d active [servers: %s] (✓%d ✗%d ⏳%d ↩%d)",
		total, active, servers,
		statusCounts[StatusSuccess], statusCounts[StatusFailed],
		statusCounts[StatusPending], statusCounts[StatusRolledBack])
}
