package core

// goal_work_summary.go — 把「EXEC 这一轮干了什么」压成有界一句话，喂给 goal
// 治理的 ADVISOR 回合（TLEvalSignal{turn_completed}.Detail → work.progress 帧）。
//
// 它**不是** team work 前缀的作者：前缀是主会话上下文（含主会话 draft）在存储侧
// 的只读装配（见 Service.noteTeamWorkPrefix / agentteam.Runtime.NoteMainContext），
// 与 TL 对话记录同一条 engine loop 口径。本文件的摘要按轮切分，天然是"第二套叙述"，
// 拿它当前缀会让前缀与对话记录分叉。
//
// 为什么在这里：EXEC 的可见会话投影是应用层事实（谁说了什么、调了哪些工具），
// 而 goal 域只接受一句有界 Detail（MaxSignalDetailRunes）——压缩是应用层职责，
// 治理域不读会话存储。
//
// 口径：
//   - 只取"最后一条 user 行之后"的内容 = 本轮，跨轮不串；
//   - 正文取最后一条非空 assistant 内容（本轮终稿），换行压成单行；
//   - 工具名按出现顺序去重追加，便于 ADVISOR 判断"它是靠证据说的还是空口"；
//   - **computer use 证据**（截图媒体引用 + 画面尺寸 + 前台窗口标题）单独抽出来
//     排在工具名之前：ADVISOR 是"看证据评审"的角色，只给它工具名
//     `computer_screenshot` 等于让它猜 EXEC 到底看到了什么；
//   - 结果按 goaldomain.MaxSignalDetailRunes 截断（正文优先，工具表其次）。

import (
	"encoding/json"
	"fmt"
	"strings"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/session"
)

// goalTurnWorkSummary 读目标会话的可见投影，返回本轮 EXEC 工作摘要（有界）。
// 只读视图（View.mu 叶子锁），不碰引擎历史——ChatStream 锁内/锁外都可安全调用。
func (service *Service) goalTurnWorkSummary(sessionID string) string {
	if service == nil || service.components.view == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	var summary string
	service.ViewMu.RLock()
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		if view == nil {
			return
		}
		summary = summarizeTurnWork(view.Conversation)
	})
	service.ViewMu.RUnlock()
	return summary
}

// summarizeTurnWork 抽取本轮（最后一条 user 行之后）的 EXEC 产出摘要。
func summarizeTurnWork(messages []Message) string {
	start := 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			start = index + 1
			break
		}
	}
	reply := ""
	tools := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	turn := messages[start:]
	for _, message := range turn {
		if message.Tool != nil && strings.TrimSpace(message.Tool.Name) != "" {
			name := strings.TrimSpace(message.Tool.Name)
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				tools = append(tools, name)
			}
			continue
		}
		if message.Role == "assistant" {
			if content := oneLine(message.Content); content != "" {
				reply = content
			}
		}
	}
	if reply == "" && len(tools) == 0 {
		return ""
	}
	detail := reply
	// 屏幕证据排在工具名之前：截断时先牺牲工具表，保住"看到了什么"。
	if screen := computerUseEvidence(turn); screen != "" {
		detail = strings.TrimSpace(detail + " | screen: " + screen)
	}
	if len(tools) > 0 {
		detail = strings.TrimSpace(detail + " | tools: " + strings.Join(tools, ", "))
	}
	return truncateRunes(detail, goaldomain.MaxSignalDetailRunes)
}

// computerUseEvidence 抽取本轮 computer use 的可审查证据：截图工具结果里的
// `media:<hash>` 引用、画面尺寸与前台窗口标题（都是 computer_screenshot 明文
// 返回的公开字段，不含像素内容）。最多保留两份画面：Detail 是"有界一句话"，
// 不是证据全文。
func computerUseEvidence(messages []Message) string {
	const (
		maxScreens      = 2
		titleRunes      = 80
		refPreviewRunes = 14
	)
	evidence := make([]string, 0, maxScreens)
	for _, message := range messages {
		if len(evidence) >= maxScreens {
			break
		}
		content := strings.TrimSpace(message.Content)
		if content == "" || !strings.Contains(content, `"ref"`) || !strings.Contains(content, `"media:`) {
			continue
		}
		var payload struct {
			Ref        string `json:"ref"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			Foreground *struct {
				Title string `json:"title"`
			} `json:"foreground"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			continue
		}
		if !strings.HasPrefix(payload.Ref, "media:") {
			continue
		}
		parts := []string{truncateRunes(payload.Ref, refPreviewRunes)}
		if payload.Width > 0 && payload.Height > 0 {
			parts = append(parts, fmt.Sprintf("%dx%d", payload.Width, payload.Height))
		}
		if payload.Foreground != nil && strings.TrimSpace(payload.Foreground.Title) != "" {
			title := oneLine(strings.ReplaceAll(truncateRunes(payload.Foreground.Title, titleRunes), `"`, "'"))
			parts = append(parts, `foreground="`+title+`"`)
		}
		evidence = append(evidence, strings.Join(parts, " "))
	}
	return strings.Join(evidence, "; ")
}

// oneLine 压掉换行/连续空白（Detail 是"有界一句话"，不是多行正文）。
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

// truncateRunes 按 rune 截断（中文不被切半）。
func truncateRunes(text string, max int) string {
	runes := []rune(text)
	if max <= 0 || len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
