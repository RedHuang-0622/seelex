package mcpstack

import (
	"fmt"
	"strings"
)

// ForPrompt generates a token-budget-aware textual summary of the MCP call history
// for LLM context injection.
//
// budget: maximum token count for the generated summary.
// The output focuses on recent calls and aggregates older ones.
func (s *MCPStack) ForPrompt(budget int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.ActiveCount() == 0 {
		return "## MCP 调用历史\n\n当前没有已记录的 MCP 调用。"
	}

	var b strings.Builder
	b.WriteString("## MCP 调用历史\n\n")

	// Metadata
	b.WriteString(fmt.Sprintf("会话: %s | 共 %d 次调用, %d 条有效",
		s.SessionID, s.TotalCount(), s.ActiveCount()))
	if s.Metadata.SessionGoal != "" {
		b.WriteString(fmt.Sprintf(" | 目标: %s", s.Metadata.SessionGoal))
	}
	b.WriteString("\n\n")

	// Estimate how many calls to show
	avgTokens := s.averageTokenCount()
	if avgTokens == 0 {
		avgTokens = 30
	}
	maxCalls := budget / avgTokens
	if maxCalls < 3 {
		maxCalls = 3
	}

	// Show recent calls
	start := max(0, s.CurrentIdx-maxCalls+1)
	showCount := s.CurrentIdx - start + 1

	b.WriteString(fmt.Sprintf("### 最近 %d 次调用\n\n", showCount))
	for i := start; i <= s.CurrentIdx; i++ {
		call := s.Calls[i]
		statusIcon := "✓"
		if call.Status == StatusFailed {
			statusIcon = "✗"
		} else if call.Status == StatusPending {
			statusIcon = "⏳"
		} else if call.Status == StatusRolledBack {
			statusIcon = "↩"
		}

		b.WriteString(fmt.Sprintf("%d. %s `%s` on `%s`\n",
			call.Seq, statusIcon, call.ToolName, call.ServerName))

		if call.ErrorMsg != "" {
			b.WriteString(fmt.Sprintf("   错误: %s\n", call.ErrorMsg))
		}
	}

	// Show undone calls count
	undoneCount := len(s.Calls) - 1 - s.CurrentIdx
	if undoneCount > 0 {
		b.WriteString(fmt.Sprintf("\n> 有 %d 条已撤销的调用\n", undoneCount))
	}

	// Group by server summary
	serverGroups := make(map[string]int)
	for _, call := range s.Calls[:s.CurrentIdx+1] {
		serverGroups[call.ServerName]++
	}
	if len(serverGroups) > 1 {
		b.WriteString("\n### 按服务器分组\n\n")
		for name, count := range serverGroups {
			b.WriteString(fmt.Sprintf("- `%s`: %d 次调用\n", name, count))
		}
	}

	return b.String()
}

func (s *MCPStack) averageTokenCount() int {
	if len(s.Calls) == 0 {
		return 30
	}
	total := 0
	for _, call := range s.Calls {
		total += max(call.TokenCount, 20)
	}
	return total / len(s.Calls)
}
