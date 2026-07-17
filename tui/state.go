package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

const CellSoftLimit = 500

type Cell struct {
	Role    string
	Content string
	Tool    *application.ToolCall
}

func (cell Cell) Render(_ int) string {
	switch cell.Role {
	case "user":
		return fmt.Sprintf("%s\n  %s", StyleUser.Render("  You"), cell.Content)
	case "assistant":
		if cell.Content == "" {
			return ""
		}
		return fmt.Sprintf("%s\n  %s", StyleAssistant.Render("  Seele"), cell.Content)
	case "tool":
		if cell.Tool == nil {
			return ""
		}
		icon := "→"
		switch cell.Tool.Status {
		case "running":
			icon = StyleTaskRunning.Render("●")
		case "success":
			icon = StyleTaskDone.Render("✓")
		case "error":
			icon = StyleError.Render("✗")
		}
		arguments := cell.Tool.Arguments
		if len(arguments) > 80 {
			arguments = arguments[:80] + "..."
		}
		line := fmt.Sprintf("  %s %s(%s)", icon, cell.Tool.Name, arguments)
		if cell.Tool.Duration > 0 && cell.Tool.Status != "running" {
			line += StyleMuted.Render(fmt.Sprintf("  %s", cell.Tool.Duration.Round(100*time.Millisecond)))
		}
		return StyleToolCall.Render(line)
	case "tool_result":
		content := cell.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		name := ""
		if cell.Tool != nil && cell.Tool.Name != "" {
			name = "[" + cell.Tool.Name + "] "
		}
		return StyleToolResult.Render("    ↳ " + name + content)
	case "system":
		if cell.Content == "" {
			return ""
		}
		return StyleSystem.Render("  ● " + cell.Content)
	case "error":
		return StyleError.Render("  ✖ " + cell.Content)
	default:
		return cell.Content
	}
}

func renderConversation(messages []application.Message, width int) string {
	if len(messages) > CellSoftLimit {
		messages = append(messages[:1:1], messages[len(messages)-CellSoftLimit+1:]...)
	}
	var builder strings.Builder
	for index, message := range messages {
		rendered := (Cell{Role: message.Role, Content: message.Content, Tool: message.Tool}).Render(width)
		if rendered == "" {
			continue
		}
		if index > 0 && builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(rendered)
	}
	return builder.String()
}
