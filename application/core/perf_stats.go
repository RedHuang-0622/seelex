package core

import (
	"encoding/json"

	"github.com/RedHuang-0622/seelex/application/model"
)

// PerfStats 返回 GUI 性能追踪钩子的后端数据面：只上报数量/体积等无内容
// 指标（不含任何 prompt/工具参数/输出文本），供前端渲染进程对照——
// DOM 节点数 ↔ 快照载荷体积 ↔ JS heap 的涨跌能直接定位"谁在吃内存"。
// GUI 每 5~10s 轮询一次即可（开销 = 一次锁内扫描 + 会话 JSON 体积估算）。
func (service *Service) PerfStats() model.PerfStats {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	snapshot := service.Core.Snapshot
	stats := model.PerfStats{
		ConversationMessages: len(snapshot.Conversation),
		HistoryWindow:        Limits().HistoryWindow,
		Revision:             snapshot.Revision,
	}
	totalChars := 0
	largest := 0
	for _, message := range snapshot.Conversation {
		size := len(message.Content)
		if message.Tool != nil {
			size += len(message.Tool.Arguments) + len(message.Tool.Result)
			if len(message.Tool.Result) > largest {
				largest = len(message.Tool.Result)
			}
			if message.Tool.Truncated {
				stats.TruncatedOutputs++
			}
		}
		if size > largest {
			largest = size
		}
		totalChars += size
	}
	stats.ConversationChars = totalChars
	stats.LargestMessageChars = largest
	// 快照整体 JSON 体积估算（前端对照 DOM/JS heap 用；估算不落盘）。
	if encoded, err := json.Marshal(snapshot); err == nil {
		stats.SnapshotBytes = len(encoded)
	}
	for _, ref := range service.components.tasks.ToolResultRefs() {
		stats.ToolResults++
		stats.ArchivedBytes += ref.Size
	}
	return stats
}
