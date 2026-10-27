package worktable

// evidence.go — 证据文本的**唯一截断实现**（工作表格行投影、项目看板行投影、
// 工具事件载荷三处共用）。
//
// 为什么落在这里：它原本作为未导出的 `truncateWorkEvidence` 住在
// `application/assembly/work_table.go`，与工作表格行投影同文件。M5-R5 把项目看板的
// 行投影下沉到 `middle/view/board` 之后，看板侧也需要同一份截断判据——而同一条
// 判据（含 `…` 省略号、按 rune 计数、`limit <= 0` 视为不限）出现在两处就会漂移，
// 因此把它提成工作表格投影模块（本包）的导出函数：`core` 与 `board` 都调这一份。
//
// 语义与搬迁前逐字一致（不含第二份判据）：limit <= 0 或字符数不超过 limit 时原样
// 返回；否则按 rune 截断并补一个 `…`。
func TruncateWorkEvidence(value string, limit int) string {
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}
