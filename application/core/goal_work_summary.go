package core

// goal_work_summary.go — 把「EXEC 这一轮干了什么」压成有界一句话，喂给 goal
// 治理的 ADVISOR 回合（TLEvalSignal{turn_completed}.Detail → work.progress 帧）。
//
// 为什么在这里：EXEC 的可见会话投影是应用层事实（谁说了什么、调了哪些工具），
// 而 goal 域只接受一句有界 Detail（MaxSignalDetailRunes）——压缩是应用层职责，
// 治理域不读会话存储。
//
// 口径：
//   - 只取"最后一条 user 行之后"的内容 = 本轮，跨轮不串；
//   - 正文取最后一条非空 assistant 内容（本轮终稿），换行压成单行；
//   - 工具名按出现顺序去重追加，便于 ADVISOR 判断"它是靠证据说的还是空口"；
//   - 结果按 goaldomain.MaxSignalDetailRunes 截断（正文优先，工具表其次）。

import (
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
	for _, message := range messages[start:] {
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
	if len(tools) > 0 {
		detail = strings.TrimSpace(detail + " | tools: " + strings.Join(tools, ", "))
	}
	return truncateRunes(detail, goaldomain.MaxSignalDetailRunes)
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
