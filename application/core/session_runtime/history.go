package session_runtime

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
)

// LoadHistoryTailWindow 尾部窗口读：先探总数（limit=0 只读 manifest），
// 再读尾部 window 条（只解析覆盖窗口的 shard）。返回窗口消息与真实总数，
// resumeSession 的 visibleHistory/TotalMessages 直接消费。
// 注（审查 #8）：探测与窗口读是两次独立加锁操作，非原子——两读之间会话
// 并发增长时 total 与窗口可能错位；恢复场景通常无并发写，影响极小（观察项）。
func (c *Coordinator) LoadHistoryTailWindow(location Location) ([]contract.EngineMessage, int, error) {
	window := c.limits().HistoryWindow
	_, total, err := c.LoadSessionHistoryRange(location.WorkspaceID, location.Meta.ID, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	offset := total - window
	if offset < 0 {
		offset = 0
	}
	history, _, err := c.LoadSessionHistoryRange(location.WorkspaceID, location.Meta.ID, offset, window)
	return history, total, err
}

// LatestUserContent 返回可见会话中最后一条 user 消息内容（内部标记跳过）。
func (c *Coordinator) LatestUserContent(messages []model.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" && !isInternalConversationMessage(messages[index], c.isInternalContent) {
			return messages[index].Content
		}
	}
	return ""
}

// HistoryContainsUser 判定 provider 历史中是否存在指定 user 内容。
func (c *Coordinator) HistoryContainsUser(history []contract.EngineMessage, content string) bool {
	content = strings.TrimSpace(c.displayUserInput(content))
	if content == "" {
		return true
	}
	for _, message := range history {
		if message.Role == "user" && strings.TrimSpace(c.displayUserInput(message.Content)) == content {
			return true
		}
	}
	return false
}

// TranscriptContainsUser 判定 transcript 事件中是否存在指定 user 内容。
func (c *Coordinator) TranscriptContainsUser(events []model.TranscriptEvent, content string) bool {
	content = strings.TrimSpace(c.displayUserInput(content))
	if content == "" {
		return true
	}
	for _, event := range events {
		if event.Role == "user" && strings.TrimSpace(c.displayUserInput(event.Content)) == content {
			return true
		}
	}
	return false
}
