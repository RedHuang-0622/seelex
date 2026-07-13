// ── 流式输出 + Engine 交互 + 历史管理 ─────────────────────
//
// 职责：
//   1. doStream → ChatStream goroutine → streamCh
//   2. Engine hooks → toolEvent → streamCh
//   3. handleStreamChunk / handleToolEvent → AppState
//   4. rebuildFromHistory / addHistoryMessage
//   5. syncView / tokensFromEngine

package tui

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/Seele/types"
	tea "github.com/charmbracelet/bubbletea"
)

// streamEventCh 让 Engine hooks 能把工具事件送回 bubbletea 事件循环
var streamEventCh chan streamChunk

// ── 流式输出（goroutine） ───────────────────────────────────────

func (m Model) doStream(input string) {
	streamEventCh = m.streamCh
	defer func() {
		streamEventCh = nil
		if r := recover(); r != nil {
			select {
			case m.streamCh <- streamChunk{done: true, err: fmt.Errorf("panic: %v", r)}:
			default:
			}
		}
	}()

	ctx := context.Background()
	_, err := m.eng.ChatStream(ctx, input, func(chunk string) {
		select {
		case m.streamCh <- streamChunk{text: chunk}:
		default:
		}
	})
	m.streamCh <- streamChunk{done: true, err: err}
}

func waitStream(ch chan streamChunk) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// ── 流式 Chunk 处理（含工具事件） ─────────────────────────────

func (m Model) handleStreamChunk(msg streamChunk) (tea.Model, tea.Cmd) {
	if msg.tool != nil {
		return m.handleToolEvent(*msg.tool)
	}
	if msg.done {
		m.state.Streaming = false
		m.viewport.Height = m.convHeight()
		if msg.err != nil {
			m.state.Conv.Add(Cell{Kind: CellError, Content: msg.err.Error()})
		} else {
			m.rebuildFromHistory()
		}
		m.syncView()
		return m, nil
	}
	m.state.Conv.AppendToLast(msg.text)
	m.syncView()
	return m, waitStream(m.streamCh)
}

// ── 工具事件处理（实时展示工具调用链） ──────────────────────

func (m Model) handleToolEvent(evt toolEvent) (tea.Model, tea.Cmd) {
	switch evt.kind {
	case "start":
		args := evt.arguments
		if len(args) > 80 {
			args = args[:80] + "..."
		}
		m.state.Conv.Add(Cell{
			Kind:    CellToolCall,
			Content: fmt.Sprintf("%s(%s)", evt.name, args),
			Extra:   evt.id,
			Status:  "running",
		})


	case "complete":
		for i := len(m.state.Conv.Cells) - 1; i >= 0; i-- {
			if m.state.Conv.Cells[i].Kind == CellToolCall && m.state.Conv.Cells[i].Extra == evt.id {
				m.state.Conv.Cells[i].Status = "success"
				if evt.err != nil {
					m.state.Conv.Cells[i].Status = "error"
				}
				break
			}
		}

		result := evt.result
		if evt.err != nil {
			result = evt.err.Error()
		}
		if len(result) > 200 {
			result = result[:200] + "..."
		}
		m.state.Conv.Add(Cell{
			Kind:    CellToolRes,
			Content: result,
			Extra:   evt.name,
		})
		m.state.Conv.Add(Cell{Kind: CellAssistant, Content: ""})
	}
	m.syncView()
	return m, waitStream(m.streamCh)
}

// ── 从 Engine History 重建会话（流结束后调用）─────────────

func (m *Model) rebuildFromHistory() {
	hist := m.eng.History()
	if len(hist) == 0 {
		return
	}
	m.state.Conv.Clear()
	m.state.Conv.Add(Cell{
		Kind:    CellSystem,
		Content: fmt.Sprintf("Seele CLI — %s", m.modelName),
	})
	for _, h := range hist {
		m.addHistoryMessage(h)
	}
}

// addHistoryMessage 将 types.Message 转为 Cell 加入 Conv
func (m *Model) addHistoryMessage(h types.Message) {
	switch h.Role {
	case "system":
		if h.Content != nil {
			m.state.Conv.Add(Cell{Kind: CellSystem, Content: *h.Content})
		}
	case "user":
		if h.Content != nil {
			m.state.Conv.Add(Cell{Kind: CellUser, Content: *h.Content})
		}
	case "assistant":
		if h.Content != nil && *h.Content != "" {
			m.state.Conv.Add(Cell{Kind: CellAssistant, Content: *h.Content})
		}
		for _, tc := range h.ToolCalls {
			args := tc.Function.Arguments
			if len(args) > 60 {
				args = args[:60] + "..."
			}
			m.state.Conv.Add(Cell{
				Kind:    CellToolCall,
				Content: fmt.Sprintf("%s(%s)", tc.Function.Name, args),
				Extra:   tc.ID,
				Status:  "success",
			})
		}
	case "tool":
		content := ""
		if h.Content != nil {
			content = *h.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
		}
		m.state.Conv.Add(Cell{
			Kind:    CellToolRes,
			Content: content,
			Extra:   h.Name,
		})
	}
}

// ── 同步视口 ─────────────────────────────────────────────────

func (m *Model) syncView() {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderConversation())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// ── Token ───────────────────────────────────────────────────────

func tokensFromEngine(eng *engine.Engine) string {
	if eng == nil {
		return "0"
	}
	tree := eng.ExportTrace()
	if tree == nil || tree.Root == nil {
		return "0"
	}
	for _, c := range tree.Root.Children {
		if c.Kind == tracer.SpanLLMCall {
			if t, ok := c.Attrs["total_tokens"]; ok {
				return t
			}
		}
	}
	return "0"
}

// ── 工具链 hooks（Engine 在 ReAct 循环中回调） ──────────────

func CreateToolHooks() *engine.LoopHooks {
	return &engine.LoopHooks{
		OnToolStart: func(_ context.Context, info engine.ToolCallInfo) {
			ch := streamEventCh
			if ch == nil {
				return
			}
			select {
			case ch <- streamChunk{
				tool: &toolEvent{
					kind: "start", name: info.Name,
					id: fmt.Sprintf("%s-%d", info.Name, info.Turn), arguments: info.Arguments,
				},
			}:
			default:
			}
		},
		OnToolComplete: func(_ context.Context, info engine.ToolCallInfo) {
			ch := streamEventCh
			if ch == nil {
				return
			}
			select {
			case ch <- streamChunk{
				tool: &toolEvent{
					kind: "complete", name: info.Name,
					id: fmt.Sprintf("%s-%d", info.Name, info.Turn),
					result: info.Result, err: info.Error,
				},
			}:
			default:
			}
		},
	}
}

// Compile guard
var _ = types.Message{}
