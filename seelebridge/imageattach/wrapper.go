package imageattach

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// DefaultPrompt 是随图那句话的默认正文。
//
// 它明确说出「这是刚截到的画面」，否则模型容易把图片当成历史里的一段装饰，
// 转而去猜屏幕内容。
const DefaultPrompt = "以下是刚刚截取的屏幕画面，请以图为准回答："

// Options 是装配 Wrapper 的可注入点；零值即可用（只挂内存里的图片本体）。
type Options struct {
	// SessionID 从 ctx 解析会话 ID；为空时用 telemetry.SessionIDFromContext。
	SessionID func(context.Context) string
	// Loader 按 ref 读取图片（会话媒体分区）；为空时只用 Attachment.Image。
	Loader func(context.Context, string) (types.ImagePart, error)
	// Prompt 覆盖 DefaultPrompt。
	Prompt string
	// OnAttach 在成功挂图后回调（诊断/事件），可为空。
	OnAttach func(sessionID string, attachments []Attachment)
	// OnDrop 在图片无法解析（ref 读不到且没有本体）时回调，可为空。
	OnDrop func(sessionID string, attachment Attachment, err error)
}

// Wrapper 在发请求前把会话里待发的画面挂上，并实现引擎侧三个窄接口。
//
// Next 是必需项；Stream / StreamEvents 为空时，流式调用退化为「一次同步 Complete
// + 单块回调」——这与引擎在没有 StreamCompleter 时的回退语义一致。
type Wrapper struct {
	Next         agent.Completer
	Stream       agent.StreamCompleter
	StreamEvents agent.StreamEventCompleter
	Pending      *Registry
	Options
}

// Complete 实现 agent.Completer。
func (w *Wrapper) Complete(ctx context.Context, messages []types.Message, tools []types.Tool) (types.Message, error) {
	if w == nil || w.Next == nil {
		return types.Message{}, errors.New("imageattach: next completer is nil")
	}
	prepared, err := w.prepare(ctx, messages)
	if err != nil {
		return types.Message{}, err
	}
	return w.Next.Complete(ctx, prepared, tools)
}

// CompleteStream 实现 agent.StreamCompleter。
func (w *Wrapper) CompleteStream(
	ctx context.Context,
	messages []types.Message,
	tools []types.Tool,
	onChunk func(string),
) (string, string, []types.ToolCall, error) {
	if w == nil {
		return "", "", nil, errors.New("imageattach: nil wrapper")
	}
	prepared, err := w.prepare(ctx, messages)
	if err != nil {
		return "", "", nil, err
	}
	if w.Stream != nil {
		return w.Stream.CompleteStream(ctx, prepared, tools, onChunk)
	}
	message, err := w.Complete(ctx, prepared, tools)
	if err != nil {
		return "", "", nil, err
	}
	content := message.Text()
	if onChunk != nil && content != "" {
		onChunk(content)
	}
	return content, message.ReasoningContent, message.ToolCalls, nil
}

// CompleteStreamEvents 实现 agent.StreamEventCompleter。
func (w *Wrapper) CompleteStreamEvents(
	ctx context.Context,
	messages []types.Message,
	tools []types.Tool,
	onEvent func(types.StreamEvent),
) (string, string, []types.ToolCall, error) {
	if w == nil {
		return "", "", nil, errors.New("imageattach: nil wrapper")
	}
	prepared, err := w.prepare(ctx, messages)
	if err != nil {
		return "", "", nil, err
	}
	if w.StreamEvents != nil {
		return w.StreamEvents.CompleteStreamEvents(ctx, prepared, tools, onEvent)
	}
	message, err := w.Complete(ctx, prepared, tools)
	if err != nil {
		return "", "", nil, err
	}
	content := message.Text()
	if onEvent != nil && content != "" {
		onEvent(types.StreamEvent{Type: types.StreamEventText, Content: content})
	}
	if onEvent != nil {
		onEvent(types.StreamEvent{Type: types.StreamEventDone})
	}
	return content, message.ReasoningContent, message.ToolCalls, nil
}

// sessionID 解析会话 ID。
func (w *Wrapper) sessionID(ctx context.Context) string {
	if w.SessionID != nil {
		return strings.TrimSpace(w.SessionID(ctx))
	}
	return strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
}

// prepare 在原始消息之后补一条带图的 user 消息；没有待随图时原样返回。
func (w *Wrapper) prepare(ctx context.Context, messages []types.Message) ([]types.Message, error) {
	sessionID := w.sessionID(ctx)
	attachments := w.Pending.Take(sessionID)
	if len(attachments) == 0 {
		return messages, nil
	}

	images := make([]types.ImagePart, 0, len(attachments))
	labels := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		image, err := w.resolve(ctx, attachment)
		if err != nil {
			if w.OnDrop != nil {
				w.OnDrop(sessionID, attachment, err)
			}
			continue
		}
		if image.Name == "" {
			image.Name = attachment.Label
		}
		images = append(images, image)
		labels = append(labels, attachmentLabel(attachment))
	}
	if len(images) == 0 {
		return messages, nil
	}

	prompt := strings.TrimSpace(w.Prompt)
	if prompt == "" {
		prompt = DefaultPrompt
	}
	notice := types.Message{Role: "user"}.
		WithText(prompt + "（" + strings.Join(labels, "；") + "）").
		WithImages(images...)

	prepared := make([]types.Message, 0, len(messages)+1)
	prepared = append(prepared, messages...)
	prepared = append(prepared, notice)
	if w.OnAttach != nil {
		w.OnAttach(sessionID, attachments)
	}
	return prepared, nil
}

// resolve 取图片本体：优先内存里的字节，否则按 ref 加载。
func (w *Wrapper) resolve(ctx context.Context, attachment Attachment) (types.ImagePart, error) {
	image := attachment.Image
	if len(image.Data) > 0 || image.URL != "" {
		return image, nil
	}
	ref := strings.TrimSpace(attachment.Ref)
	if ref == "" || w.Loader == nil {
		return types.ImagePart{}, fmt.Errorf("imageattach: attachment %q has no bytes and no loader", ref)
	}
	loaded, err := w.Loader(ctx, ref)
	if err != nil {
		return types.ImagePart{}, fmt.Errorf("imageattach: load %q: %w", ref, err)
	}
	return loaded, nil
}

// attachmentLabel 给出人可读的来源标签。
func attachmentLabel(attachment Attachment) string {
	label := strings.TrimSpace(attachment.Label)
	if label != "" {
		return label
	}
	if ref := strings.TrimSpace(attachment.Ref); ref != "" {
		return ref
	}
	return "已附加图片"
}
