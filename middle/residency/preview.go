// Package residency 是中间层"常驻与预算"的落点（plan §2.4：`middle/residency/`）：
// 快照预算的**截断判据**先落在这里（`runtime_projection` / LRU 家族随 R6 续做搬入）。
//
// 依赖纪律：本包只做**纯计算**——进程级上限（`seelex.yaml` 的 limits 段）与归档
// 读数口都留在宿主侧取数，算完的值由宿主写回（与 `middle/view` 同一条口径）。
package residency

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Preview 把超限内容截断为快照预览：取前 limit 字节（按 UTF-8 rune 边界安全切割，
// 避免切坏多字节字符），并追加一行可读的省略说明（供 TUI/纯文本面展示，GUI 另按
// TotalChars 渲染"加载完整输出"）。返回 (预览, 是否截断)。
//
// limit <= 0 = **不截断**（原样返回）：调用方负责给出正数预算（进程级上限的取数与
// 默认值都在宿主侧，本包不读配置）。
func Preview(content string, limit int) (string, bool) {
	if limit <= 0 || len(content) <= limit {
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
