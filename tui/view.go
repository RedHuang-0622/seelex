package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/tui/splash"
)

const shortcutsBarH = 1

// awaitingApprovalStatus 是会话级待批状态的稳定字面量（与 model.SessionStatus
// awaiting_approval 一致；TUI 只消费展示口径，不参与归属判定）。
const awaitingApprovalStatus = "awaiting_approval"

func (model Model) convHeight() int {
	return max(model.height-model.topPanelH()-model.planPanelH()-model.midPanelH()-model.bottomPanelH(), 4)
}
func (model Model) topPanelH() int {
	height := 2
	if len(model.pendingSessions()) > 0 {
		height++
	}
	return height
}
func (model Model) planPanelH() int {
	return PlanPanelHeight(model.snapshot.Runtime.Plan, model.snapshot.Runtime.Effort)
}
func (model Model) midPanelH() int {
	height := 0
	if model.suggMode && len(currentSuggestions(model)) > 0 {
		height += suggWindowSize + 3
	}
	if model.snapshot.Chat.Running {
		height++ // ● receiving
	}
	if n := len(model.snapshot.Chat.InputQueue); n > 0 {
		height += 1 + min(n, 10) // queue: 标题 + 条目
	}
	if model.uiError != "" {
		height++
	}
	return height
}
func (model Model) bottomPanelH() int { return model.textareaHeight + shortcutsBarH + 2 }

func (model Model) View() string {
	if !model.ready {
		return splash.Gradient + "\n\n  Loading...\n"
	}
	if model.showLogo {
		return splash.Render(model.width, model.height, model.snapshot.Runtime.Model)
	}
	var builder strings.Builder
	builder.WriteString(model.renderStatusBar())
	builder.WriteString("\n")
	if panel := model.renderPendingApprovals(); panel != "" {
		builder.WriteString(panel)
		builder.WriteString("\n")
	}
	if panel := PlanPanel(model.snapshot.Runtime.Plan, model.snapshot.Runtime.Effort, model.width); panel != "" {
		builder.WriteString(panel)
		builder.WriteString("\n")
	}
	if model.snapshot.Interaction != nil {
		model.viewport.SetContent(model.renderInteraction())
	} else {
		model.viewport.SetContent(model.renderConversation())
	}
	builder.WriteString(model.viewport.View())
	builder.WriteString("\n")
	builder.WriteString(StyleSep.Render(strings.Repeat("─", model.width)))
	builder.WriteString("\n")
	if model.snapshot.Chat.Running {
		elapsed := time.Since(model.snapshot.Chat.StartedAt).Round(100 * time.Millisecond)
		builder.WriteString(StyleStatus.Render(fmt.Sprintf("  ● receiving  %s", elapsed)))
		builder.WriteString("\n")
	}
	builder.WriteString(model.renderQueue())
	if model.uiError != "" {
		builder.WriteString(StyleError.Render("  ✖ " + model.uiError))
		builder.WriteString("\n")
	}
	if model.suggMode {
		suggestions := currentSuggestions(model)
		if len(suggestions) > 0 {
			builder.WriteString(renderSuggestions(suggestions, model.suggIdx, model.suggOffset, model.width, model.textarea.Value()))
		}
	}
	builder.WriteString(StyleInputBox.Width(max(model.width-2, 1)).Render(model.renderInputLine()))
	builder.WriteString("\n")
	builder.WriteString(model.renderShortcuts())
	return builder.String()
}

// pendingSessions 返回目录中处于 awaiting_approval 的会话行（跨会话待批
// 计数的目录面；Snapshot.Sessions 是后端目录联合镜像，非视图单格神谕）。
func (model Model) pendingSessions() []application.SessionInfo {
	if len(model.snapshot.Sessions) == 0 {
		return nil
	}
	var pending []application.SessionInfo
	for _, item := range model.snapshot.Sessions {
		if item.Status == awaitingApprovalStatus {
			pending = append(pending, item)
		}
	}
	return pending
}

// pendingApprovalCount 返回跨会话待批总数（状态行计数；单格审批仍只表达
// 当前视图会话的 Interaction，后台会话待批只计数、不做单格展开）。
func (model Model) pendingApprovalCount() int {
	total := 0
	for _, item := range model.pendingSessions() {
		if item.ApprovalCount > 0 {
			total += item.ApprovalCount
		} else {
			total++
		}
	}
	return total
}

// renderPendingApprovals 渲染待批会话的最小提示行：跨会话计数 + 短 ID。
// 这是 TUI 的最小承载面（无会话侧栏；不做平行会话管理）——切到目标会话后，
// 该会话的审批经既有单格 Interaction 呈现与 ResolveInteraction 处理。
func (model Model) renderPendingApprovals() string {
	pending := model.pendingSessions()
	if len(pending) == 0 {
		return ""
	}
	ids := make([]string, 0, len(pending))
	for _, item := range pending {
		id := item.ID
		if len(id) > 8 {
			id = id[len(id)-8:]
		}
		ids = append(ids, id)
	}
	total := model.pendingApprovalCount()
	label := StyleTaskRunning.Render(fmt.Sprintf("  ⚠ 待审批 %d 项", total))
	detail := StyleMuted.Render("  " + strings.Join(ids, " "))
	return label + detail
}

func (model Model) renderInputLine() string {
	// 使用 textarea.View() 而不是 Value()，这样光标跟随实际位置
	return FormatInput(model.textarea.View())
}

func (model Model) renderConversation() string {
	return renderConversation(model.snapshot.Conversation, model.width)
}

func effortBadge(effort string) string {
	colors := map[string]lipgloss.Color{
		"lite":   lipgloss.Color("241"), // 灰
		"medium": lipgloss.Color("220"), // 金
		"high":   lipgloss.Color("75"),  // 蓝
		"max":    lipgloss.Color("198"), // 紫红
	}
	c, ok := colors[effort]
	if !ok {
		c = lipgloss.Color("241")
	}
	return lipgloss.NewStyle().Foreground(c).Render("E:" + effort)
}

func (model Model) renderStatusBar() string {
	provider := model.snapshot.Runtime.Provider
	if provider == "" {
		provider = "round-robin"
	}
	parts := []string{provider}
	eff := model.snapshot.Runtime.Effort
	if eff == "" {
		eff = "high"
	}
	parts = append(parts, effortBadge(eff))
	if plugin := model.snapshot.Runtime.Plugin; plugin != "" && plugin != "default" {
		parts = append(parts, plugin)
	}
	tokens := model.snapshot.Runtime.Tokens
	if tokens == "" {
		tokens = "0"
	}
	parts = append(parts, "tok:"+tokens)
	if model.snapshot.Chat.Running {
		parts = append(parts, time.Since(model.snapshot.Chat.StartedAt).Round(time.Second).String())
		if q := model.snapshot.Chat.QueuedCount; q > 0 {
			parts = append(parts, fmt.Sprintf("queue:%d", q))
		}
	}
	if sessionID := model.snapshot.Session.ID; len(sessionID) > 8 {
		parts = append(parts, sessionID[len(sessionID)-8:])
	}
	if pending := model.pendingApprovalCount(); pending > 0 {
		parts = append(parts, fmt.Sprintf("待批:%d", pending))
	}
	right := strings.Join(parts, "  ")
	left := StyleBanner.Render(" ◆ Seele") + StyleMuted.Render(fmt.Sprintf("  %s", model.snapshot.Runtime.Model))
	spacing := max(model.width-lipgloss.Width(left)-lipgloss.Width(right)-4, 1)
	return StyleStatus.Render(left + strings.Repeat(" ", spacing) + right)
}
func (model Model) renderQueue() string {
	queue := model.snapshot.Chat.InputQueue
	if len(queue) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(StyleMuted.Render("  queue:"))
	b.WriteString("\n")
	for i, q := range queue {
		line := fmt.Sprintf("  %d. ", i+1)
		disp := strings.ReplaceAll(q, "\n", " ")
		if len(disp) > 60 {
			disp = disp[:60] + "..."
		}
		b.WriteString(StyleMuted.Render(line + disp))
		b.WriteString("\n")
	}
	return b.String()
}

func (model Model) renderInteraction() string {
	interaction := model.snapshot.Interaction
	if interaction == nil {
		return ""
	}
	var builder strings.Builder
	title := interaction.Title
	if interaction.Question != "" {
		title = interaction.Question
	}
	builder.WriteString(StyleConfirm.Render("  " + title))
	builder.WriteString("\n\n")
	if interaction.ToolName != "" {
		builder.WriteString(StyleMuted.Render("  Tool: " + interaction.ToolName))
		builder.WriteString("\n")
	}
	if interaction.Preview != "" {
		builder.WriteString(StyleToolResult.Render("  " + interaction.Preview))
		builder.WriteString("\n")
	}
	for index, option := range interaction.Options {
		prefix, style := "  ", StyleChoiceInactive
		if index == model.interactionSel {
			prefix, style = " ▸ ", StyleChoiceActive
		}
		line := fmt.Sprintf("%s%d. %s", prefix, index+1, option.Label)
		if option.Description != "" {
			line += StyleMuted.Render("  " + option.Description)
		}
		builder.WriteString(style.Render(line))
		builder.WriteString("\n")
	}
	builder.WriteString(StyleMuted.Render("  ↑↓ Enter选择 Esc取消 数字键快捷跳转"))
	return builder.String()
}

func (Model) renderShortcuts() string {
	items := []string{"Ctrl+C copy", "Ctrl+V paste", "Alt+E effort", "Ctrl+Q quit", "drag select"}
	var builder strings.Builder
	for index, item := range items {
		if index > 0 {
			builder.WriteString("  ")
		}
		builder.WriteString(StyleShortcut.Render(item))
	}
	return StyleShortcutBar.Render(builder.String())
}

func (model *Model) syncView() {
	if !model.ready {
		return
	}
	atBottom := model.viewport.AtBottom()
	if model.snapshot.Interaction != nil {
		model.viewport.SetContent(model.renderInteraction())
	} else {
		model.viewport.SetContent(model.renderConversation())
	}
	model.viewport.Height = model.convHeight()
	if atBottom {
		model.viewport.GotoBottom()
	}
}

// FormatInput 格式化输入行。
func FormatInput(textareaView string) string {
	return textareaView
}

func max(first, second int) int {
	if first > second {
		return first
	}
	return second
}
