package chat

import "strings"

// VisibleOutputStream removes model reasoning blocks before they cross the
// application-to-frontend boundary. It keeps a small delimiter suffix so a
// tag split across streaming chunks never flashes in the UI.
type VisibleOutputStream struct {
	requestID string
	inThink   bool
	pending   string
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

func (stream *VisibleOutputStream) Consume(chunk string) string {
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
