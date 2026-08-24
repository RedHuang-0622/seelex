package search

import (
	"bytes"
	"fmt"
)

// FormatResponse 把规范搜索结果格式化为模型可直接消费的 Markdown：
// 先输出 AI 摘要（如有），再逐条输出标题、URL 与摘要。
func FormatResponse(resp SearchResponse) string {
	var buf bytes.Buffer
	if resp.Answer != "" {
		buf.WriteString("## AI 摘要\n\n")
		buf.WriteString(resp.Answer)
		buf.WriteString("\n\n")
	}
	if len(resp.Items) > 0 {
		buf.WriteString("## 搜索结果\n\n")
		for i, r := range resp.Items {
			fmt.Fprintf(&buf, "%d. **%s**\n", i+1, r.Title)
			fmt.Fprintf(&buf, "   URL: %s\n", r.URL)
			if r.Content != "" {
				fmt.Fprintf(&buf, "   %s\n", r.Content)
			}
			buf.WriteString("\n")
		}
	}
	return buf.String()
}
