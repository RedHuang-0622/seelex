package chat

import "strings"

// VisibleOutputStream removes model reasoning blocks before they cross the
// application-to-frontend boundary. It keeps a small delimiter suffix so a
// tag split across streaming chunks never flashes in the UI.
type VisibleOutputStream struct {
	requestID string
	inThink   bool
	pending   string
	// text 累积本请求截至当前的可见正文（think 块已剥离）。回看历史（可见窗口
	// 未贴尾）时流式增量按设计不进窗口，工具轮说明正文的按迭代归位只能从这里读
	// （见 application/core/chat.go 的 streamedAssistantTextLocked）。
	text strings.Builder
}

func NewVisibleOutputStream(requestID string) *VisibleOutputStream {
	return &VisibleOutputStream{requestID: requestID}
}

// RequestID 返回该输出流绑定的请求 ID（跨包只读访问）。
func (stream *VisibleOutputStream) RequestID() string {
	if stream == nil {
		return ""
	}
	return stream.requestID
}

// Text 返回本请求已累积的可见正文（空串 = 还没有可见正文）。
func (stream *VisibleOutputStream) Text() string {
	if stream == nil {
		return ""
	}
	return stream.text.String()
}

func (stream *VisibleOutputStream) Consume(chunk string) string {
	visible := stream.filter(chunk)
	if visible != "" {
		stream.text.WriteString(visible)
	}
	return visible
}

// filter 是 think 块过滤本体：返回本分片里真正可见的正文。
func (stream *VisibleOutputStream) filter(chunk string) string {
	input := stream.pending + chunk
	stream.pending = ""
	var visible strings.Builder
	for input != "" {
		if stream.inThink {
			at := strings.Index(input, "</think>")
			if at < 0 {
				stream.pending = trailingTagPrefix(input, "</think>")
				return visible.String()
			}
			input = input[at+len("</think>"):]
			stream.inThink = false
			continue
		}
		at := strings.Index(input, "<think>")
		if at < 0 {
			pending := trailingTagPrefix(input, "<think>")
			visible.WriteString(strings.TrimSuffix(input, pending))
			stream.pending = pending
			return visible.String()
		}
		visible.WriteString(input[:at])
		input = input[at+len("<think>"):]
		stream.inThink = true
	}
	return visible.String()
}

func StripThoughtBlocks(value string) string {
	stream := NewVisibleOutputStream("")
	return stream.Consume(value)
}

func trailingTagPrefix(value, tag string) string {
	max := len(tag) - 1
	if len(value) < max {
		max = len(value)
	}
	for size := max; size > 0; size-- {
		if strings.HasSuffix(value, tag[:size]) {
			return tag[:size]
		}
	}
	return ""
}
