package imageattach

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

type recordingCompleter struct {
	calls    int
	messages [][]types.Message
	reply    types.Message
}

func (c *recordingCompleter) Complete(_ context.Context, messages []types.Message, _ []types.Tool) (types.Message, error) {
	c.calls++
	c.messages = append(c.messages, append([]types.Message(nil), messages...))
	return c.reply, nil
}

type recordingStreamCompleter struct {
	*recordingCompleter
	chunks int
}

func (c *recordingStreamCompleter) CompleteStream(
	_ context.Context,
	messages []types.Message,
	_ []types.Tool,
	onChunk func(string),
) (string, string, []types.ToolCall, error) {
	c.calls++
	c.messages = append(c.messages, append([]types.Message(nil), messages...))
	if onChunk != nil {
		onChunk("streamed")
		c.chunks++
	}
	return "streamed", "reasoning", nil, nil
}

func pngAttachment(label string) Attachment {
	return Attachment{
		Ref:   "media:abc",
		Label: label,
		File:  types.FilePart{Kind: types.FileKindImage, MimeType: "image/png", Data: []byte{1, 2, 3}, Width: 1280, Height: 720},
	}
}

func TestRegistryTakeIsOnceAndSessionScoped(t *testing.T) {
	registry := NewRegistry()
	registry.Add("s1", pngAttachment("screen-1"))
	registry.Add("s1", pngAttachment("screen-2"))
	registry.Add("s2", pngAttachment("other"))
	registry.Add("", pngAttachment("nowhere"))

	if got := registry.Len("s1"); got != 2 {
		t.Fatalf("Len(s1) = %d, want 2", got)
	}
	taken := registry.Take("s1")
	if len(taken) != 2 {
		t.Fatalf("Take(s1) = %d 张, want 2", len(taken))
	}
	if got := registry.Len("s1"); got != 0 {
		t.Fatalf("Take 之后 Len(s1) = %d, want 0", got)
	}
	if again := registry.Take("s1"); again != nil {
		t.Fatalf("二次 Take(s1) = %v, want nil（至多送一次）", again)
	}
	if got := len(registry.Take("s2")); got != 1 {
		t.Fatalf("Take(s2) = %d 张, want 1", got)
	}
	if got := registry.Len(""); got != 0 {
		t.Fatalf("空 sessionID 必须丢弃, Len(\"\") = %d", got)
	}
}

func TestWrapperAttachesPendingImageExactlyOnce(t *testing.T) {
	next := &recordingCompleter{reply: types.Message{Role: "assistant"}.WithText("ok")}
	registry := NewRegistry()
	registry.Add("s1", pngAttachment("screenshot 1280x720"))
	wrapper := &Wrapper{
		Next:    next,
		Pending: registry,
		Options: Options{SessionID: func(context.Context) string { return "s1" }},
	}
	original := []types.Message{types.Message{Role: "user"}.WithText("看看屏幕")}
	ctx := context.Background()

	if _, err := wrapper.Complete(ctx, original, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(next.messages) != 1 {
		t.Fatalf("next 被调用 %d 次, want 1", len(next.messages))
	}
	sent := next.messages[0]
	if len(sent) != len(original)+1 {
		t.Fatalf("发出 %d 条消息, want %d（原历史 + 一条带图 user 消息）", len(sent), len(original)+1)
	}
	notice := sent[len(sent)-1]
	if notice.Role != "user" || notice.ImageCount() != 1 {
		t.Fatalf("附加消息 = role=%q images=%d, want user/1", notice.Role, notice.ImageCount())
	}
	if !strings.Contains(notice.Text(), DefaultPrompt) || !strings.Contains(notice.Text(), "screenshot 1280x720") {
		t.Fatalf("附加消息正文 = %q, 应含提示语与来源标签", notice.Text())
	}
	if got := notice.Files[0].Name; got != "screenshot 1280x720" {
		t.Fatalf("图片溯源名 = %q, want 标签", got)
	}
	if sent[0].ImageCount() != 0 {
		t.Fatal("原始历史不应被改写")
	}

	// 第二次调用：队列已空，消息必须原样送出（同一张图不重复占 token）。
	if _, err := wrapper.Complete(ctx, original, nil); err != nil {
		t.Fatalf("第二次 Complete: %v", err)
	}
	if got := len(next.messages[1]); got != len(original) {
		t.Fatalf("第二次发出 %d 条消息, want %d", got, len(original))
	}
}

func TestWrapperLoadsImageByRef(t *testing.T) {
	next := &recordingCompleter{}
	registry := NewRegistry()
	registry.Add("s1", Attachment{Ref: "media:ref-only"})
	loaded := 0
	wrapper := &Wrapper{
		Next:    next,
		Pending: registry,
		Options: Options{
			SessionID: func(context.Context) string { return "s1" },
			Loader: func(_ context.Context, ref string) (types.FilePart, error) {
				loaded++
				if ref != "media:ref-only" {
					return types.FilePart{}, errors.New("unexpected ref")
				}
				return types.FilePart{Kind: types.FileKindImage, MimeType: "image/png", Data: []byte{9}}, nil
			},
		},
	}
	if _, err := wrapper.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("Loader 调用 %d 次, want 1", loaded)
	}
	sent := next.messages[0]
	if len(sent) != 1 || sent[0].ImageCount() != 1 {
		t.Fatalf("加载后的消息 = %+v, want 一条带图消息", sent)
	}
	if got := sent[0].Files[0].MimeType; got != "image/png" {
		t.Fatalf("加载图片 mime = %q, want image/png", got)
	}
}

func TestWrapperDropsUnresolvableAttachmentWithoutBreakingRequest(t *testing.T) {
	next := &recordingCompleter{}
	registry := NewRegistry()
	registry.Add("s1", Attachment{Ref: "media:missing"})
	dropped := 0
	wrapper := &Wrapper{
		Next:    next,
		Pending: registry,
		Options: Options{
			SessionID: func(context.Context) string { return "s1" },
			Loader: func(context.Context, string) (types.FilePart, error) {
				return types.FilePart{}, errors.New("boom")
			},
			OnDrop: func(string, Attachment, error) { dropped++ },
		},
	}
	original := []types.Message{types.Message{Role: "user"}.WithText("hi")}
	if _, err := wrapper.Complete(context.Background(), original, nil); err != nil {
		t.Fatalf("读图失败不应让整次请求失败: %v", err)
	}
	if dropped != 1 {
		t.Fatalf("OnDrop 调用 %d 次, want 1", dropped)
	}
	if got := len(next.messages[0]); got != len(original) {
		t.Fatalf("无可解析图片时发出 %d 条消息, want %d", got, len(original))
	}
}

func TestWrapperStreamPaths(t *testing.T) {
	base := &recordingCompleter{}
	streamer := &recordingStreamCompleter{recordingCompleter: base}
	registry := NewRegistry()
	registry.Add("s1", pngAttachment("screenshot"))
	wrapper := &Wrapper{
		Next:    base,
		Stream:  streamer,
		Pending: registry,
		Options: Options{SessionID: func(context.Context) string { return "s1" }},
	}
	content, _, _, err := wrapper.CompleteStream(context.Background(), nil, nil, func(string) {})
	if err != nil || content != "streamed" {
		t.Fatalf("CompleteStream = %q, %v", content, err)
	}
	if streamer.chunks != 1 || len(streamer.messages[0]) != 1 || streamer.messages[0][0].ImageCount() != 1 {
		t.Fatalf("流式下一层未收到带图消息: %+v", streamer.messages)
	}

	// 没有 Stream 时退化为「一次同步 Complete + 单块回调」，与引擎的回退语义一致。
	registry.Add("s1", pngAttachment("screenshot"))
	syncNext := &recordingCompleter{reply: types.Message{Role: "assistant"}.WithText("sync")}
	fallback := &Wrapper{
		Next:    syncNext,
		Pending: registry,
		Options: Options{SessionID: func(context.Context) string { return "s1" }},
	}
	var chunks []string
	content, _, _, err = fallback.CompleteStream(context.Background(), nil, nil, func(delta string) { chunks = append(chunks, delta) })
	if err != nil || content != "sync" {
		t.Fatalf("回退 CompleteStream = %q, %v", content, err)
	}
	if len(chunks) != 1 || chunks[0] != "sync" {
		t.Fatalf("回退回调 = %v, want [sync]", chunks)
	}
	var events []types.StreamEventType
	if _, _, _, err := fallback.CompleteStreamEvents(context.Background(), nil, nil, func(event types.StreamEvent) {
		events = append(events, event.Type)
	}); err != nil {
		t.Fatalf("CompleteStreamEvents: %v", err)
	}
	if len(events) != 2 || events[0] != types.StreamEventText || events[1] != types.StreamEventDone {
		t.Fatalf("回退事件 = %v, want [text done]", events)
	}
}
