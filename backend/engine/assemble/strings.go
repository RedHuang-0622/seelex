package assemble

// strings.go 放装配层自己的零散字符串助手。
//
// 它们原本挂在 `runtime_goal_tl.go` 尾部，随该文件的评估器本体一起搬进 goal 域；但
// **用法不止 goal 域**（`runtime_role_prompt.go` 的提示词优化与
// `runtime_role_turn.go` 的回合读数都用），所以留在本层并单独成文件，避免下次搬
// 域文件时又被连带删掉。
//
// 与仓库里别处同名助手的**不并**说明（AGENTS §8.2：判据相同才并）：本仓另有四处
// `truncateRunes`（`backend/tool/registry`、`kernel/task`、`application/assembly`、
// `backend/engine/context/seelectx`）与两处 `stringPtr`（`backend/tool/node`、
// `backend/engine/context/seelectx` 测试），判据**并不相同**——有没有省略号、要不要
// TrimSpace、`<= 0` 是返回空串还是原样返回，各处都不一样。它们是"长得像"而不是
// "同一件事"，并成一份反而会改掉别处的既有语义。

// stringPtr 返回字符串的指针（`types.Message.Content` 是 `*string`）。
func stringPtr(value string) *string { return &value }

// truncateRunes 把一段原文压到 max 个 rune 并加省略号（错误文案里的原文摘录用）。
func truncateRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}
