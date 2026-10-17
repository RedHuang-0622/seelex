package store

// project_board_markdown.go — 项目看板**人读件**（`BOARD.md`）的确定性渲染。
//
// 口径（与投影存储面同一份契约）：
//
//  1. **纯函数**：`ProjectBoardMarkdown` 只吃"已折叠的行"，不读盘、不取时刻、不改入参；
//     同一份行必得同一份字节（行已按 (Kind, Key, Session) 稳定排序）。
//  2. **不落本机标识**（契约 §3 三条硬纪律）：不渲染 `scope.project`（`ws-<nano>`）、
//     不渲染 `at`（时刻不是看板要看的东西，且每次追加都变 ⇒ 无意义 diff）、
//     所有文本过 `sanitizeKnownText`（剔 `ws-<nano>` 与盘符绝对路径）。
//  3. **不做判定**（I3）：状态/名称/依赖**原样搬运**，空就留空——不补"pending"、
//     不翻译词表、不排序成"优先级"。渲染只是"把行摆成人能读的样子"。
//  4. **空则退场**（I6）：没有行 ⇒ 不产文件（由 refreshProjectBoardMarkdown 管）。
//
// 为什么它在这儿而不是 application/core：它是投影**存储面**（`<项目根>/.seelex/board/`）
// 的一份落盘产物，与 `worktable.jsonl` / `teamwork.jsonl` 同一个目录、同一条生命周期；
// 渲染只依赖行信封（不含任何领域类型）⇒ 不引入第二条取数链。

import (
	"strings"
)

// ProjectBoardMarkdown 把**折叠后的投影行**渲染成人读看板。
//
// 分两段：工作（kind=work）一张表；编排（kind=milestone / milestone_item）按里程碑
// 分节，节内挂它的工作项表。段的顺序与行顺序一致（行已稳定排序）⇒ 字节稳定。
func ProjectBoardMarkdown(rows []ProjectBoardRow) []byte {
	var work []ProjectBoardRow
	var milestones []ProjectBoardRow
	itemsByMilestone := map[string][]ProjectBoardRow{}
	for _, row := range rows {
		switch row.Kind {
		case ProjectBoardKindWork:
			work = append(work, row)
		case ProjectBoardKindMilestone:
			milestones = append(milestones, row)
		case ProjectBoardKindMilestoneItem:
			parent := boardText(row.Payload, "milestone")
			itemsByMilestone[parent] = append(itemsByMilestone[parent], row)
		}
	}

	var builder strings.Builder
	builder.WriteString("# 项目看板（Project Board）\n\n")
	builder.WriteString("> 派生投影：由 task / teamwork 事实生成，可删除并重建；不是事实源，不参与任何判定。\n\n")

	if len(work) > 0 {
		builder.WriteString("## 工作\n\n")
		builder.WriteString("| 状态 | 类型 | 任务 | 会话 | 说明 |\n")
		builder.WriteString("|---|---|---|---|---|\n")
		for _, row := range work {
			title := boardText(row.Payload, "task")
			if title == "" {
				title = boardText(row.Payload, "id")
			}
			builder.WriteString("| " + boardCell(boardText(row.Payload, "status")) +
				" | " + boardCell(boardText(row.Payload, "kind")) +
				" | " + boardCell(title) +
				" | " + boardCell(row.Scope.Session) +
				" | " + boardCell(boardText(row.Payload, "description")) + " |\n")
		}
		builder.WriteString("\n")
	}

	if len(milestones) > 0 {
		builder.WriteString("## 编排\n\n")
		for _, row := range milestones {
			id := boardText(row.Payload, "id")
			name := boardText(row.Payload, "name")
			heading := "### 里程碑 `" + id + "`"
			if name != "" {
				heading += "： " + name
			}
			builder.WriteString(heading + "\n\n")
			writer := newBoardFacts(builder.WriteString)
			writer("状态", boardText(row.Payload, "status"))
			writer("依赖", strings.Join(boardTextList(row.Payload, "depends_on"), ", "))
			if closed, _ := row.Payload["team_closed"].(bool); closed {
				writer("整队", "已收口")
			}
			content := boardText(row.Payload, "content")
			builder.WriteString("\n")
			if content != "" {
				builder.WriteString(content + "\n\n")
			}
			items := itemsByMilestone[id]
			if len(items) > 0 {
				builder.WriteString("| 状态 | 工作项 | 名称 | 角色 | 会话 | 目标 |\n")
				builder.WriteString("|---|---|---|---|---|---|\n")
				for _, item := range items {
					builder.WriteString("| " + boardCell(boardText(item.Payload, "status")) +
						" | " + boardCell(boardText(item.Payload, "id")) +
						" | " + boardCell(boardText(item.Payload, "name")) +
						" | " + boardCell(boardText(item.Payload, "role")) +
						" | " + boardCell(boardText(item.Payload, "session_id")) +
						" | " + boardCell(boardText(item.Payload, "goal")) + " |\n")
				}
				builder.WriteString("\n")
			}
		}
	}
	return []byte(builder.String())
}

// newBoardFacts 返回一个"写一条 - key：value"的小写入器（空值不写，保持确定性）。
func newBoardFacts(write func(string) (int, error)) func(key, value string) {
	return func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		_, _ = write("- " + key + "： `" + value + "`\n")
	}
}

// boardText 取载荷里的文本字段并清洗（非字符串 / 空 ⇒ 空串）。
func boardText(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return sanitizeKnownText(value)
}

// boardTextList 取载荷里的字符串数组字段并清洗（保持原顺序，空项剔除）。
func boardTextList(payload map[string]any, key string) []string {
	values, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		if cleaned := sanitizeKnownText(text); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

// boardCell 把一段文本规整成 markdown 表格单元格（换行折成空格、竖线转义）。
func boardCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—" // 空单元格在表格里看不见，用破折号明确"这里没有值"
	}
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
