package reference

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/types"
)

// ReadCompressedTurnHandler handles the read_compressed_turn model tool. The
// compressed-turn archive remains owned by the Session transcript store; this
// adapter only validates the segment request, loads its authoritative content,
// renders the turns and applies bounded pagination.
func (application *Application) ReadCompressedTurnHandler(argsJSON string) (string, error) {
	var input struct {
		SegmentID string `json:"segment_id"`
		Offset    int    `json:"offset,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Contains  string `json:"contains,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_compressed_turn: invalid JSON: %w", err)
	}
	input.SegmentID = strings.TrimSpace(input.SegmentID)
	if input.SegmentID == "" || input.Offset < 0 {
		return "", errors.New("read_compressed_turn: segment_id is required and offset must be non-negative")
	}
	if input.Limit <= 0 {
		input.Limit = application.deps.ReferencePageSize()
	}
	if max := application.deps.MaxReferencePageSize(); max > 0 && input.Limit > max {
		input.Limit = max
	}

	sessionID, workspaceID := application.deps.CompressedTurnView()
	content, err := application.deps.LoadCompressedTurn(
		workspaceID,
		sessionID,
		application.deps.CompressedTurnPrefix+input.SegmentID,
	)
	if err != nil {
		return "", fmt.Errorf("read_compressed_turn: %w", err)
	}
	var messages []types.Message
	if err := json.Unmarshal([]byte(content), &messages); err != nil {
		return "", fmt.Errorf("read_compressed_turn: decode stored turns: %w", err)
	}
	rendered := renderCompressedTurns(messages)
	return pageCompressedTurns(rendered, input.Offset, input.Limit, input.Contains)
}

// renderCompressedTurns renders archived turns as role-tagged text. Assistant
// tool-call turns retain the tool names so a page remains readable without the
// original structured wire messages.
func renderCompressedTurns(messages []types.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		switch {
		case message.Role == "user":
			builder.WriteString("[user] ")
			builder.WriteString(content)
		case message.Role == "assistant" && len(message.ToolCalls) == 0:
			builder.WriteString("[assistant] ")
			builder.WriteString(content)
		case message.Role == "assistant":
			builder.WriteString("[assistant tools] ")
			names := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				names = append(names, call.Function.Name)
			}
			builder.WriteString(strings.Join(names, ", "))
		case message.Role == "tool":
			builder.WriteString("[tool ")
			builder.WriteString(message.Name)
			builder.WriteString("] ")
			builder.WriteString(content)
		default:
			builder.WriteString("[")
			builder.WriteString(message.Role)
			builder.WriteString("] ")
			builder.WriteString(content)
		}
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

// pageCompressedTurns filters rendered lines and returns one rune-bounded page.
func pageCompressedTurns(rendered string, offset, limit int, contains string) (string, error) {
	if contains != "" {
		lines := strings.Split(rendered, "\n")
		filtered := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.Contains(line, contains) {
				filtered = append(filtered, line)
			}
		}
		rendered = strings.Join(filtered, "\n")
	}
	if offset >= utf8.RuneCountInString(rendered) {
		return "", errors.New("read_compressed_turn: offset exceeds stored content")
	}
	runes := []rune(rendered)
	if offset >= len(runes) {
		return "", errors.New("read_compressed_turn: offset exceeds stored content")
	}
	end := offset + limit
	if end > len(runes) {
		end = len(runes)
	}
	page := string(runes[offset:end])
	if end < len(runes) {
		page += "\n...[truncated; use read_compressed_turn with offset to continue]"
	}
	return page, nil
}
