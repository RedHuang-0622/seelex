package mcpstack

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// TraceProvider exports the MCP call trace as a ContextSnapshot for LLM
// context injection. Unlike the full Provider interface (which requires
// importing seelexctx/provider and creates a cycle), this is a simple
// snapshot builder that can be wrapped at the seelebridge level.
type TraceProvider struct {
	stack *MCPStack
}

// NewTraceProvider creates a TraceProvider wrapping the given MCPStack.
func NewTraceProvider(stack *MCPStack) *TraceProvider {
	return &TraceProvider{stack: stack}
}

// Name returns a stable identifier for this provider.
func (p *TraceProvider) Name() string { return "mcptrace" }

// BuildSnapshot exports the current MCP trace state as a ContextSnapshot.
// Returns the snapshot and any findings/pending work for LLM context.
func (p *TraceProvider) BuildSnapshot() (*snapshot.ContextSnapshot, error) {
	p.stack.mu.RLock()
	defer p.stack.mu.RUnlock()

	snap := &snapshot.ContextSnapshot{
		Findings: make([]string, 0, 2),
	}

	snap.Findings = append(snap.Findings, FormatSummary(p.stack))

	for _, call := range p.stack.Calls[:p.stack.CurrentIdx+1] {
		if call.Status == StatusPending {
			snap.PendingWork = append(snap.PendingWork,
				fmt.Sprintf("[MCP] %s/%s (pending)", call.ServerName, call.ToolName))
		}
	}

	return snap, nil
}

// Compact reduces the trace to a minimal summary.
func (p *TraceProvider) Compact(snap *snapshot.ContextSnapshot, budget int) (*snapshot.ContextSnapshot, error) {
	if snap == nil {
		return nil, fmt.Errorf("mcpstack: compact: nil snapshot")
	}

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
