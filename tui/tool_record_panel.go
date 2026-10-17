package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// tool_record_panel.go — **工具记录**的 TUI 只读面（2026-10-15）。
//
// 生态位：与 GUI「状态 → 工具记录」**同源**——两边读的都是
// `Application.ToolRecordBoard()`（当前视图会话的会话级读数，落该会话自己的
// `tool-record.jsonl`），
// 终端里不再折一遍统计（那是第二份事实）。面板不新增只对终端生效的业务事实，
// 也不做任何写动作（开关是 GUI 设置的事：TUI 面板是只读面，见 tui/README.md 的边界）。
//
// 为什么走**可选读面 + 类型断言**（与 queueController 同一手法）：`AppController`
// 是 TUI 的最小契约，假实现不必为了一个只读面板长出空方法；未实现时整块退场
// （不留空壳），而不是显示一行"未装配"。

// toolRecordReader 是 AppController 的**可选**工具记录读面。
type toolRecordReader interface {
	ToolRecordBoard() application.ToolRecordBoard
}

// toolRecordLines 渲染工具记录：一行表头（开关 + 合计成功/失败）+ 每工具一行
// 成功/失败/占比 + 失败最多的原因（饼图在 GUI 里有，终端里给最要紧的那一格）。
//
// 空读数（没记过）→ nil：不追加空壳。行数上限与其它面板一致（panelLineLimit）。
func (model Model) toolRecordLines() []string {
	reader, ok := model.app.(toolRecordReader)
	if !ok {
		return nil
	}
	board := reader.ToolRecordBoard()
	if !board.Enabled || board.Total == 0 || len(board.Tools) == 0 {
		return nil
	}
	width := model.textLimit()
	header := fmt.Sprintf("  ◆ 工具记录  共 %d 次（成功 %d / 失败 %d）", board.Total, board.Success, board.Failure)
	lines := []string{StyleTaskRunning.Render(oneLine(header, width))}
	budget := panelLineLimit - len(lines)
	for index, stat := range board.Tools {
		if index >= budget {
			lines = append(lines, StyleMuted.Render(fmt.Sprintf("  ·  还有 %d 个工具（看 GUI 的柱形图看全量）", len(board.Tools)-index)))
			break
		}
		// 一行一格：工具名 + 成功/失败 + 成功率 + 失败最多的那个原因。
		row := fmt.Sprintf("  ·  %-14s 成功 %5d  失败 %5d", stat.Tool, stat.Success, stat.Failure)
		if stat.Total > 0 {
			row += fmt.Sprintf("（成功 %.1f%%）", float64(stat.Success)/float64(stat.Total)*100)
		}
		if reason, count := topReason(stat.Reasons); reason != "" {
			row += fmt.Sprintf("  主因 %s×%d", toolRecordReasonLabel(reason), count)
		}
		lines = append(lines, StyleMuted.Render(oneLine(row, width)))
	}
	if board.LedgerError != "" {
		lines = append(lines, StyleMuted.Render(oneLine("  ⚠ 台账落盘失败（计数只在内存里）："+board.LedgerError, width)))
	}
	return clampLines(lines, panelLineLimit)
}

// topReason 取失败最多的原因（并列取词表序在前的那一个，读数确定）。
func topReason(reasons []application.ToolRecordReason) (string, int) {
	best, bestCount := "", 0
	for _, reason := range reasons {
		if reason.Count > bestCount {
			best, bestCount = reason.Reason, reason.Count
		}
	}
	return best, bestCount
}

// toolRecordReasonLabel 把原因标识翻成终端里的短标签。词表只有一份（契约
// dto.ToolReason*），本表只负责中文短标签——认不得的标识照原样显示，不吞掉事实。
func toolRecordReasonLabel(reason string) string {
	switch reason {
	case "permission":
		return "权限"
	case "timeout":
		return "超时"
	case "refused":
		return "业务拒绝"
	case "not-found":
		return "目标不存在"
	case "invalid-args":
		return "入参不合法"
	case "conflict":
		return "状态冲突"
	case "transport":
		return "通道故障"
	case "command-failed":
		return "命令失败"
	case "other":
		return "其它"
	default:
		return reason
	}
}

// shortToolRecordTime 只保留日期与分钟（终端一行放不下秒）。
func shortToolRecordTime(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Local().Format("01-02 15:04")
}

// toolRecordReasonSummary 把原因表折成一行（导出给将来的终端详表用；现在只在
// toolRecordLines 之外没有第二个调用方——留着是为了"原因明细"有唯一落点时直接复用）。
func toolRecordReasonSummary(reasons []application.ToolRecordReason) string {
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%s×%d", toolRecordReasonLabel(reason.Reason), reason.Count))
	}
	return strings.Join(parts, " ")
}
