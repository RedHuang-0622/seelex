package mcpstack

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── Compile-time checks ─────────────────────────────────────────

var (
	_ provider.Provider    = (*TraceProvider)(nil)
	_ provider.Compactable = (*TraceProvider)(nil)
)

// TraceProvider implements provider.Provider, feeding the MCP call trace
// into the LLM context chain alongside other providers.
//
// This enables the LLM to be aware of all previous MCP calls in the session,
// including their arguments, results, and error states.
type TraceProvider struct {
	stack *MCPStack
	name  string
}

// NewProvider creates a TraceProvider wrapping the given MCPStack.
func NewProvider(stack *MCPStack) *TraceProvider {
	return &TraceProvider{
		stack: stack,
		name:  "mcptrace",
	}
}

func (p *TraceProvider) Name() string { return p.name }

// Export builds a ContextSnapshot from the current MCP call trace.
//
// The snapshot includes:
//   - Findings: formatted trace summary
//   - PendingWork: any calls still in StatusPending
func (p *TraceProvider) Export(_ context.Context) (*snapshot.ContextSnapshot, error) {
	p.stack.mu.RLock()
	defer p.stack.mu.RUnlock()

	snap := &snapshot.ContextSnapshot{
		Findings: make([]string, 0, 2),
	}

	// Add structural summary
	snap.Findings = append(snap.Findings, FormatSummary(p.stack))

	// Add pending calls as pending work
	for _, call := range p.stack.Calls[:p.stack.CurrentIdx+1] {
		if call.Status == StatusPending {
			snap.PendingWork = append(snap.PendingWork,
				fmt.Sprintf("[MCP] %s/%s (pending)", call.ServerName, call.ToolName))
		}
	}

	return snap, nil
}

// Compact reduces the trace to a minimal summary respecting token budget.
func (p *TraceProvider) Compact(snap *snapshot.ContextSnapshot, budget int) (*snapshot.ContextSnapshot, error) {
	if snap == nil {
		return nil, fmt.Errorf("mcpstack: compact: nil snapshot")
	}

	// Keep only the last MCP-related finding
	var lastTrace string
	for _, f := range snap.Findings {
		if len(f) > 4 && f[:4] == "MCP " {
			lastTrace = f
		}
	}

	result := &snapshot.ContextSnapshot{}
	if lastTrace != "" && budget >= 50 {
		result.Findings = []string{lastTrace}
	}

	return result, nil
}
