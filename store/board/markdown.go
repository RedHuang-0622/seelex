package board

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store/internal/textsanitize"
)

// Markdown renders folded projection rows as deterministic BOARD.md bytes.
func Markdown(rows []dto.ProjectBoardRow) []byte {
	var work []dto.ProjectBoardRow
	var milestones []dto.ProjectBoardRow
	itemsByMilestone := map[string][]dto.ProjectBoardRow{}
	for _, row := range rows {
		switch row.Kind {
		case KindWork:
			work = append(work, row)
		case KindMilestone:
			milestones = append(milestones, row)
		case KindMilestoneItem:
			parent := payloadText(row.Payload, "milestone")
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
			title := payloadText(row.Payload, "task")
			if title == "" {
				title = payloadText(row.Payload, "id")
			}
			builder.WriteString("| " + cell(payloadText(row.Payload, "status")) +
				" | " + cell(payloadText(row.Payload, "kind")) +
				" | " + cell(title) +
				" | " + cell(row.Scope.Session) +
				" | " + cell(payloadText(row.Payload, "description")) + " |\n")
		}
		builder.WriteString("\n")
	}
	if len(milestones) > 0 {
		builder.WriteString("## 编排\n\n")
		for _, row := range milestones {
			id := payloadText(row.Payload, "id")
			name := payloadText(row.Payload, "name")
			heading := "### 里程碑 `" + id + "`"
			if name != "" {
				heading += "： " + name
			}
			builder.WriteString(heading + "\n\n")
			writeFact := factsWriter(builder.WriteString)
			writeFact("状态", payloadText(row.Payload, "status"))
			writeFact("依赖", strings.Join(payloadTextList(row.Payload, "depends_on"), ", "))
			if closed, _ := row.Payload["team_closed"].(bool); closed {
				writeFact("整队", "已收口")
			}
			content := payloadText(row.Payload, "content")
			builder.WriteString("\n")
			if content != "" {
				builder.WriteString(content + "\n\n")
			}
			items := itemsByMilestone[id]
			if len(items) == 0 {
				continue
			}
			builder.WriteString("| 状态 | 工作项 | 名称 | 角色 | 会话 | 目标 |\n")
			builder.WriteString("|---|---|---|---|---|---|\n")
			for _, item := range items {
				builder.WriteString("| " + cell(payloadText(item.Payload, "status")) +
					" | " + cell(payloadText(item.Payload, "id")) +
					" | " + cell(payloadText(item.Payload, "name")) +
					" | " + cell(payloadText(item.Payload, "role")) +
					" | " + cell(payloadText(item.Payload, "session_id")) +
					" | " + cell(payloadText(item.Payload, "goal")) + " |\n")
			}
			builder.WriteString("\n")
		}
	}
	return []byte(builder.String())
}

func factsWriter(write func(string) (int, error)) func(key, value string) {
	return func(key, value string) {
		if strings.TrimSpace(value) != "" {
			_, _ = write("- " + key + "： `" + value + "`\n")
		}
	}
}

func payloadText(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return textsanitize.KnownText(value)
}

func payloadTextList(payload map[string]any, key string) []string {
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
		if cleaned := textsanitize.KnownText(text); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func cell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—"
	}
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
