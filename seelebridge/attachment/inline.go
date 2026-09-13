// Package attachment 把「手上有的附件」变成「端点真的能收的载体」。
//
// 为什么要把 image 与 document 分开处理：真机实测（api.deepseek.com / deepseek-v4-flash）
// 图片通道是通的（image_url：模型答出 "Red"），但**文档通道根本不存在**：
//
//   - OpenAI 官方嵌套形状 {"type":"file","file":{...}} 被端点当成空对象，
//     回 400 "file must have a file_id or file_data"（它读的是**扁平**字段）；
//   - 扁平 {"type":"file","file_data":...,"filename":...} 能过校验，但只认
//     webp/png/jpeg/gif（错误原文列了白名单），PDF/text 一律 400；
//   - /files 上传（唯一支持 purpose=user_data）同样只收图片，PDF 400；
//   - Anthropic 兼容端点的 document block 是 200 却内容不进模型（回答为空）。
//
// 所以文档必须降级而不是硬塞：能当文本读的就内联成文本（同问法真机验证过——
// 模型原样答出附件里的编号），不能读的给诊断，绝不静默丢弃。
package attachment

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/types"
)

// Limit 是单个文档内联进提问的上限：内联吃上下文也吃 token，超限只给诊断。
const Limit = 256 << 10

// Plan 是一次附件处置的结果。
//
//   - Files：原样交给 provider 附件通道的部分（目前只有图片，通道已被真机验证）；
//   - Text：降级成文本的文档，调用方把它拼进提问即可；
//   - Notes：逐条处置说明，写给人看（日志 / UI），不发给模型。
type Plan struct {
	Files []types.FilePart
	Text  string
	Notes []string
}

// PlanFor 按 FileKind 分流：图片原样保留，文档走文本内联或诊断。
func PlanFor(files []types.FilePart) Plan {
	var plan Plan
	var inlined []string
	for _, file := range files {
		if file.IsImage() {
			plan.Files = append(plan.Files, file)
			continue
		}
		text, note := inlineDocument(file)
		plan.Notes = append(plan.Notes, note)
		if text != "" {
			inlined = append(inlined, text)
		}
	}
	plan.Text = strings.Join(inlined, "\n\n")
	return plan
}

// TextSuffix 返回可直接拼到提问后面的内联文本（没有内联内容时为空串）。
func (p Plan) TextSuffix() string {
	if p.Text == "" {
		return ""
	}
	return "\n\n" + p.Text
}

// inlineDocument 把一个文档附件降级成内联文本；返回空文本时 note 说明为什么没发。
func inlineDocument(file types.FilePart) (string, string) {
	label := fmt.Sprintf("%s(%s)", displayName(file.Name), mimeOf(file))
	switch {
	case len(file.Data) == 0 && file.URL != "":
		return "", fmt.Sprintf("文档 %s：只有 URL（%s），本层不下载，未发送", label, file.URL)
	case len(file.Data) == 0:
		return "", fmt.Sprintf("文档 %s：没有内容，未发送", label)
	case len(file.Data) > Limit:
		return "", fmt.Sprintf("文档 %s：%d 字节超过内联上限 %d 字节，未发送", label, len(file.Data), Limit)
	case !inlineType(mimeOf(file)):
		return "", fmt.Sprintf("文档 %s：不是可直接内联的文本类型，未发送（需要文本提取器或先渲染成图片）", label)
	case !utf8.Valid(file.Data):
		return "", fmt.Sprintf("文档 %s：含非 UTF-8 字节（疑似二进制），未发送", label)
	}
	body := strings.TrimRight(string(file.Data), "\n")
	return fmt.Sprintf("<attachment name=%q mime=%q>\n%s\n</attachment>", displayName(file.Name), mimeOf(file), body),
		fmt.Sprintf("文档 %s：端点没有文档通道，已内联为文本（%d 字节）", label, len(file.Data))
}

// mimeOf 取附件的 MIME：没写就按内容嗅探，避免「没填 MIME」被当成不可内联。
func mimeOf(file types.FilePart) string {
	if mime := strings.TrimSpace(file.MimeType); mime != "" {
		return mime
	}
	if len(file.Data) == 0 {
		return "application/octet-stream"
	}
	detected := http.DetectContentType(file.Data)
	return strings.TrimSpace(strings.SplitN(detected, ";", 2)[0])
}

// inlineType 判断该 MIME 能不能安全地当文本读：text/* 一律可以，
// 结构化文本按后缀或白名单放行，其他（PDF、docx、二进制）一概不放。
func inlineType(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	if strings.HasSuffix(mime, "+json") || strings.HasSuffix(mime, "+xml") {
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml",
		"application/toml", "application/javascript", "application/x-javascript",
		"application/sql", "application/x-ndjson", "application/x-sh", "application/csv":
		return true
	}
	return false
}

// displayName 保证诊断与内联标注里的文件名不会把标注本身弄坏。
func displayName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\"", "'"))
	if name == "" {
		return "(未命名)"
	}
	return name
}
