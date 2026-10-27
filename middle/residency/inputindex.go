package residency

// inputindex.go — 「会话内全量用户输入索引」（SA-D 右侧索引）的**纯装配判据**。
//
//	右侧导航刻度 = 用户输入（一条刻度 = 会话里的一条用户输入），且是**全量**：包含尚未加载到前端
//	窗口的早期轮次。索引只带**有界摘要**，不带正文，因此可以整份下发。
//
// 落点沿革：2026-10-16 随 **M5-R6-input-index** 接收者转移从
// `application/assembly/session_input_index.go` 归位（plan §2.4 的 `session_input_index.go` → `middle/residency/`）；
// "从哪读"（会话存储的 conversation 区间读 / record 回退）与三条"算不算用户输入"的宿主谓词
// （会话恢复前缀、provider-only 内部行、展示正文还原）仍留在宿主（它们是后端域的判据）。

import (
	"strings"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// InputIndexSummaryLimit 是刻度摘要的字符上限（rune）：悬停只需要一行提示，正文不回传
// （"零正文全量读"：整份索引只有摘要，摘要长度有界）。
const InputIndexSummaryLimit = 60

// SummarizeInputText 把正文压成有界摘要：空白折叠 + rune 截断（末尾省略号）。
// 返回摘要与原文 rune 数（`Chars > len([]rune(Summary))` 即已截断）。
func SummarizeInputText(text string, limit int) (string, int) {
	if limit <= 0 {
		limit = InputIndexSummaryLimit
	}
	flat := strings.Join(strings.Fields(text), " ")
	chars := len([]rune(flat))
	if chars <= limit {
		return flat, chars
	}
	runes := []rune(flat)
	return string(runes[:limit]) + "…", chars
}

// AlignLoadedInputTexts 把「窗口内已加载的用户输入」对齐到全量输入序列：先求最长前缀的**连续整段命中**
// （自尾部向前搜索，窗口通常是尾部窗口），未命中再退一步用更短前缀（最新一条输入可能尚未落盘/尚未进入索引）。
// 返回命中起点与长度；无法命中返回 (0,0)——宁可不标 Loaded（前端仍按 DOM 命中兜底），也不能标错。
func AlignLoadedInputTexts(texts, loaded []string) (int, int) {
	if len(texts) == 0 || len(loaded) == 0 || len(loaded) > len(texts) {
		return 0, 0
	}
	for length := len(loaded); length >= 1; length-- {
		for start := len(texts) - length; start >= 0; start-- {
			matched := true
			for offset := 0; offset < length; offset++ {
				if texts[start+offset] != loaded[offset] {
					matched = false
					break
				}
			}
			if matched {
				return start, length
			}
		}
	}
	return 0, 0
}

// BuildInputIndex 从完整可见会话构建全量用户输入索引（纯函数）：
//   - 只保留 `userText` 判定为"用户输入"的行（宿主谓词：非 user 行、内部标记、会话恢复摘要、
//     空白行都不算；skill 包装输入按"用户看到的正文"还原）；
//   - loaded 是当前已加载窗口内的用户输入正文（顺序一致），命中段标 Loaded；
//   - 摘要按 limit 截断（rune），Chars 保留原文长度。
func BuildInputIndex(conversation []model.Message, loaded []string, limit int, userText func(model.Message) string) []model.SessionInputIndexRow {
	if limit <= 0 {
		limit = InputIndexSummaryLimit
	}
	rows := make([]model.SessionInputIndexRow, 0, len(conversation))
	texts := make([]string, 0, len(conversation))
	for offset, message := range conversation {
		text := userText(message)
		if text == "" {
			continue
		}
		summary, chars := SummarizeInputText(text, limit)
		rows = append(rows, model.SessionInputIndexRow{
			Round:         len(rows) + 1,
			MessageID:     message.ID,
			Offset:        offset,
			RoundID:       message.RoundID,
			Seq:           message.UnitSeq,
			RoleName:      message.RoleName,
			RoleSessionID: message.RoleSessionID,
			CreatedAt:     message.CreatedAt,
			Summary:       summary,
			Chars:         chars,
		})
		texts = append(texts, text)
	}
	if start, length := AlignLoadedInputTexts(texts, loaded); length > 0 {
		for index := start; index < start+length && index < len(rows); index++ {
			rows[index].Loaded = true
		}
	}
	return rows
}
