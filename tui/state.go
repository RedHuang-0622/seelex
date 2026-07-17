// ── 核心状态模型：Cell → Conversation → AppState ─────────────
// 对应 Elm Architecture 的 State Store 层

package tui

import (
	"fmt"
	"strings"
	"time"
)

// ── Cell Kind ───────────────────────────────────────────────────

type CellKind string

const (
	CellUser      CellKind = "user"
	CellAssistant CellKind = "assistant"
	CellToolCall  CellKind = "tool_call"
	CellToolRes   CellKind = "tool_result"
	CellSystem    CellKind = "system"
	CellError     CellKind = "error"
)

// ── Cell：对话中的最小渲染单元 ─────────────────────────────────

type Cell struct {
	Kind     CellKind
	Content  string
	Extra    string // 工具名 / ID 等元信息
	Status   string // tool_call: "running" | "success" | "error"
	Duration time.Duration
}

func (c Cell) Render(width int) string {
	switch c.Kind {
	case CellUser:
		return fmt.Sprintf("%s\n  %s",
			StyleUser.Render("  You"),
			c.Content)

	case CellAssistant:
		if c.Content == "" {
			return ""
		}
		return fmt.Sprintf("%s\n  %s",
			StyleAssistant.Render("  Seele"),
			c.Content)

	case CellToolCall:
		icon := "→"
		switch c.Status {
		case "running":
			icon = StyleTaskRunning.Render("●")
		case "success":
			icon = StyleTaskDone.Render("✓")
		case "error":
			icon = StyleError.Render("✗")
		}
		line := fmt.Sprintf("  %s %s", icon, c.Content)
		if c.Duration > 0 && c.Status != "running" {
			line += StyleMuted.Render(fmt.Sprintf("  %s",
				c.Duration.Round(time.Millisecond*100)))
		}
		return StyleToolCall.Render(line)

	case CellToolRes:
		prefix := ""
		if c.Extra != "" {
			prefix = "[" + c.Extra + "] "
		}
		str := c.Content
		if len(str) > 120 {
			str = str[:120] + "..."
		}
		return StyleToolResult.Render("    ↳ " + prefix + str)

	case CellSystem:
		if c.Content == "" {
			return ""
		}
		return StyleSystem.Render("  ● " + c.Content)

	case CellError:
		return StyleError.Render("  ✖ " + c.Content)
	}
	return ""
}

// CellSoftLimit — 对话 Cell 上限（低配机器友好）
const CellSoftLimit = 500

// ── Conversation：对话（Cell 有序列表）────────────────────────

type Conversation struct {
	Cells []Cell
}

func (c *Conversation) Add(cell Cell) {
	c.Cells = append(c.Cells, cell)
	// 超出上限时保留首条 system 消息 + 最近 N-1 条
	if len(c.Cells) > CellSoftLimit {
		keep := make([]Cell, 0, CellSoftLimit)
		keep = append(keep, c.Cells[0])                                // 首条（welcome）
		keep = append(keep, c.Cells[len(c.Cells)-CellSoftLimit+1:]...) // 最近条
		c.Cells = keep
	}
}

func (c *Conversation) Clear() {
	c.Cells = nil
}

func (c *Conversation) LastCell() *Cell {
	if len(c.Cells) == 0 {
		return nil
	}
	return &c.Cells[len(c.Cells)-1]
}

// AppendToLast 向最后一个 Cell 追加内容（流式增量用）
func (c *Conversation) AppendToLast(text string) {
	last := c.LastCell()
	if last == nil {
		return
	}
	last.Content += text
}

func (c *Conversation) Render(width int) string {
	var b strings.Builder
	for i, cell := range c.Cells {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(cell.Render(width))
	}
	return b.String()
}

// ── AppState：全局状态 ──────────────────────────────────────────

type AppState struct {
	Conv Conversation

	ModelName string

	Streaming bool
}

func NewAppState(modelName string) AppState {
	return AppState{
		ModelName: modelName,
		Conv: Conversation{
			Cells: []Cell{
				{Kind: CellSystem, Content: fmt.Sprintf("Seele CLI — %s", modelName)},
			},
		},
	}
}

// Keep types alive
var _ = time.Time{}
