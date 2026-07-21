// Package mcpstack provides a generic, immutable MCP call trace that ALL
// MCP interactions must go through. It serves as the single source of truth
// for every tool call made to any MCP server, supporting:
//
//   - Traceability: every MCP call is recorded with full params + result
//   - Reviewability: history can be inspected, filtered, and fed back to the LLM
//   - Undo/Redo: via CurrentIdx pointer movement (domain-specific undo not handled here)
//   - Persistence: atomic JSON-serialized file storage
//   - Provider integration: exports trace context into Seelex's Provider chain
//
// This is NOT CAD-specific. All MCP servers (CAD, architecture, medical, chemistry)
// pass through this same middleware layer.
//
// Architecture (双栈):
//   Stack 1 — MCPCallLog: append-only record of every call (immutable history)
//   Stack 2 — Interceptor: wraps MCP calls, hooks before/after (middleware)
package mcpstack

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ── Sentinel errors ─────────────────────────────────────────────

var (
	ErrStackBottom = fmt.Errorf("mcpstack: already at bottom")
	ErrStackTop    = fmt.Errorf("mcpstack: already at top")
	ErrEmptyStack  = fmt.Errorf("mcpstack: stack is empty")
)

// ── CallStatus ──────────────────────────────────────────────────

// CallStatus tracks the lifecycle of an MCP tool call.
type CallStatus int

const (
	StatusPending   CallStatus = iota // Submitted but not yet responded
	StatusSuccess                     // Completed successfully
	StatusFailed                      // Execution error
	StatusRolledBack                  // Reverted (conceptual, domain-specific)
)

func (s CallStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusSuccess:
		return "success"
	case StatusFailed:
		return "failed"
	case StatusRolledBack:
		return "rolled_back"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ── MCPCall ─────────────────────────────────────────────────────

// MCPCall records a single MCP tool invocation — the atomic unit of the trace.
// It is domain-agnostic: "Op" is just the MCP tool name, "Params" is arbitrary JSON.
type MCPCall struct {
	ID         string          `json:"id"`                   // UUID v4, globally unique
	Seq        int             `json:"seq"`                  // Sequence number (1-based)
	Timestamp  time.Time       `json:"timestamp"`            // Call creation time
	ServerName string          `json:"server_name"`          // Target MCP server (e.g. "freecad", "chem-sim")
	ToolName   string          `json:"tool_name"`            // MCP tool name (e.g. "sketch_rectangle", "simulate_binding")
	Args       json.RawMessage `json:"args"`                 // Call arguments as JSON
	Result     json.RawMessage `json:"result,omitempty"`     // Call result (set after completion)
	ErrorMsg   string          `json:"error_msg,omitempty"`   // Error message if failed
	Status     CallStatus      `json:"status"`               // Current status
	AIBacklink string          `json:"ai_backlink,omitempty"` // AI message ID that triggered this call
	TokenCount int             `json:"token_count,omitempty"` // Token estimate for the recorded data
}

// ── StackMetadata ───────────────────────────────────────────────

// StackMetadata carries session-level metadata for the MCP trace.
// It is intentionally minimal and generic — domain-specific metadata
// should be stored in the MCP server's own context, not here.
type StackMetadata struct {
	SessionGoal string `json:"session_goal,omitempty"` // High-level session goal
	Domain      string `json:"domain,omitempty"`       // Optional domain hint ("cad", "chem", …)
}

// ── MCPStack ────────────────────────────────────────────────────

// MCPStack is the immutable, serializable trace of all MCP calls.
// It is the single source of truth for every MCP interaction.
type MCPStack struct {
	mu sync.RWMutex `json:"-"`

	SessionID  string            `json:"session_id"`            // Session identifier
	CreatedAt  time.Time         `json:"created_at"`            // Creation timestamp
	UpdatedAt  time.Time         `json:"updated_at"`            // Last modification timestamp
	Calls      []MCPCall         `json:"calls"`                 // Append-only call history
	CurrentIdx int               `json:"current_idx"`           // Current state pointer (-1 = empty)
	Metadata   StackMetadata     `json:"metadata"`              // Session metadata
	Tags       map[string]string `json:"tags,omitempty"`        // Arbitrary tags

	autoSavePath string `json:"-"` // Path for automatic persistence
}

// ── Options ─────────────────────────────────────────────────────

type Option func(*MCPStack)

func WithSessionID(id string) Option {
	return func(s *MCPStack) { s.SessionID = id }
}

func WithAutoSave(path string) Option {
	return func(s *MCPStack) { s.autoSavePath = path }
}

func WithMetadata(md StackMetadata) Option {
	return func(s *MCPStack) { s.Metadata = md }
}

func WithTags(tags map[string]string) Option {
	return func(s *MCPStack) { s.Tags = tags }
}

// New creates an empty MCPStack.
func New(opts ...Option) *MCPStack {
	now := time.Now().UTC()
	s := &MCPStack{
		SessionID:  fmt.Sprintf("mcp-%s", now.Format("20060102-150405")),
		CreatedAt:  now,
		UpdatedAt:  now,
		Calls:      make([]MCPCall, 0, 64),
		CurrentIdx: -1,
		Tags:       make(map[string]string),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ── Core operations ─────────────────────────────────────────────

// Record appends a completed MCP call to the stack.
// If the current position is not at the top, calls after CurrentIdx are discarded.
func (s *MCPStack) Record(call MCPCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Discard calls after current position (branched history)
	if s.CurrentIdx < len(s.Calls)-1 {
		s.Calls = s.Calls[:s.CurrentIdx+1]
	}

	call.Seq = len(s.Calls) + 1
	if call.Timestamp.IsZero() {
		call.Timestamp = time.Now().UTC()
	}
	if call.Status == 0 && call.Status != StatusPending {
		call.Status = StatusPending
	}

	s.Calls = append(s.Calls, call)
	s.CurrentIdx = len(s.Calls) - 1
	s.UpdatedAt = time.Now().UTC()
	return s.autoSave()
}

// Undo moves CurrentIdx back by one. Returns the undone call.
// This is a pointer movement only — actual rollback is domain-specific.
func (s *MCPStack) Undo() (*MCPCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CurrentIdx < 0 || len(s.Calls) == 0 {
		return nil, ErrStackBottom
	}
	if s.CurrentIdx == 0 {
		s.CurrentIdx = -1
		s.UpdatedAt = time.Now().UTC()
		_ = s.autoSave()
		return &s.Calls[0], nil
	}
	call := &s.Calls[s.CurrentIdx]
	s.CurrentIdx--
	s.UpdatedAt = time.Now().UTC()
	_ = s.autoSave()
	return call, nil
}

// Redo moves CurrentIdx forward by one. Returns the redone call.
func (s *MCPStack) Redo() (*MCPCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Calls) == 0 {
		return nil, ErrEmptyStack
	}
	if s.CurrentIdx >= len(s.Calls)-1 {
		return nil, ErrStackTop
	}
	s.CurrentIdx++
	call := &s.Calls[s.CurrentIdx]
	s.UpdatedAt = time.Now().UTC()
	_ = s.autoSave()
	return call, nil
}

// Current returns the call at CurrentIdx, or nil if the stack is empty.
func (s *MCPStack) Current() (*MCPCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Calls) == 0 {
		return nil, ErrEmptyStack
	}
	if s.CurrentIdx < 0 {
		return nil, ErrStackBottom
	}
	return &s.Calls[s.CurrentIdx], nil
}

// Peek returns the call at CurrentIdx+offset without moving the pointer.
func (s *MCPStack) Peek(offset int) (*MCPCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Calls) == 0 {
		return nil, ErrEmptyStack
	}
	idx := s.CurrentIdx + offset
	if idx < 0 || idx >= len(s.Calls) {
		return nil, fmt.Errorf("mcpstack: offset %d out of range", offset)
	}
	return &s.Calls[idx], nil
}

// ActiveCount returns the number of active (not undone) calls.
func (s *MCPStack) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.CurrentIdx + 1
}

// TotalCount returns the total number of calls (including undone ones).
func (s *MCPStack) TotalCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.Calls)
}

// ── Query helpers ───────────────────────────────────────────────

// ByServer returns all calls (up to CurrentIdx) that targeted a given server.
func (s *MCPStack) ByServer(serverName string) []MCPCall {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []MCPCall
	for _, call := range s.Calls[:s.CurrentIdx+1] {
		if call.ServerName == serverName {
			result = append(result, call)
		}
	}
	return result
}

// ByTool returns all calls (up to CurrentIdx) for a given tool name.
func (s *MCPStack) ByTool(toolName string) []MCPCall {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []MCPCall
	for _, call := range s.Calls[:s.CurrentIdx+1] {
		if call.ToolName == toolName {
			result = append(result, call)
		}
	}
	return result
}

// Latest returns the N most recent active calls.
func (s *MCPStack) Latest(n int) []MCPCall {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.CurrentIdx < 0 {
		return nil
	}
	start := max(0, s.CurrentIdx-n+1)
	result := make([]MCPCall, 0, s.CurrentIdx-start+1)
	result = append(result, s.Calls[start:s.CurrentIdx+1]...)
	return result
}

// ── Internal ────────────────────────────────────────────────────

func (s *MCPStack) autoSave() error {
	if s.autoSavePath == "" {
		return nil
	}
	return s.save(s.autoSavePath)
}

