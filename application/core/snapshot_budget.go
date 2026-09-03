package core

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
)

// snapshotToolOutputLimit 返回可见会话快照单条工具输出的字符预算
// （seelex.yaml limits 段 snapshot_tool_output_chars，默认 8000）。
// 消费点：handleToolCompleteObserved（实时路径）与 appendHistoryLocked
// （会话恢复路径）。provider 侧历史不受此值影响，仍由
// max_tool_result_chars 单独约束。
func snapshotToolOutputLimit() int {
	limit := limits.Get().SnapshotToolOutputChars
	if limit <= 0 {
		limit = 8000
	}
	return limit
}

// toolOutputPreview 把超限工具输出截断为快照预览：取前 limit 字节（按
// UTF-8 rune 边界安全切割，避免切坏多字节字符），并追加一行可读的省略
// 说明（供 TUI/纯文本面展示，GUI 另按 TotalChars 渲染"加载完整输出"）。
// 返回 (预览, 是否截断)。
func toolOutputPreview(content string, limit int) (string, bool) {
	if limit <= 0 {
		limit = snapshotToolOutputLimit()
	}
	if len(content) <= limit {
		return content, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	preview := strings.TrimRight(content[:cut], " \t\r\n")
	total := utf8.RuneCountInString(content)
	return preview + "\n… 快照已截断（完整输出 " + strconv.Itoa(total) + " 字符）", true
}

// boundToolResultForSnapshot 生成进入可见会话快照的工具输出（调用方持有
// Core.ViewMu；StoreToolResultLocked 与 pendingToolResults 均要求持锁）：
//   - 未超预算 → 原样返回（visible=content, ref=""）；
//   - 超预算 → 全文归档为 result_ref（复用 read_tool_result 的持久化
//     通道：pending 内存态 + 会话落盘，前端可随时经 ToolResultContent
//     分页读回），快照只放预览 + 引用元数据。
//
// 这是"谁在吃内存"根因链的治本一环：60KB 级别的全文不再随每次快照/
// 事件进入渲染进程，DOM 与 JS heap 只持有 ≤ 8KB 的预览。
func (service *Service) boundToolResultForSnapshot(name, content string) (visible string, ref string, truncated bool, totalChars int) {
	limit := snapshotToolOutputLimit()
	totalChars = utf8.RuneCountInString(content)
	if len(content) <= limit {
		return content, "", false, totalChars
	}
	stored := service.components.tasks.StoreToolResultLocked(name, content)
	visible, _ = toolOutputPreview(content, limit)
	return visible, stored.Ref, true, totalChars
}
