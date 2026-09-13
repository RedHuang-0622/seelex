package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/agent"

	"github.com/RedHuang-0622/seelex/seelebridge/imageattach"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// AttachImage 把一张画面挂进「下一次模型请求随图」队列。
//
// 这是截屏 / 看图类工具的落点：工具负责把图片落进会话媒体分区并给出 ref，
// 引擎侧不需要知道图片的存在——Completer 接缝会把它挂到下一次请求上，而且
// 只挂一次（见 imageattach.Registry 的 Take 语义），避免反复占用 token 与配额。
func (r *Runtime) AttachImage(ctx context.Context, attachment imageattach.Attachment) {
	if r == nil || r.images == nil {
		return
	}
	r.images.Add(seeletelemetry.SessionIDFromContext(ctx), attachment)
}

// PendingImageCount 返回当前会话还有多少张待随图（诊断与断言用）。
func (r *Runtime) PendingImageCount(ctx context.Context) int {
	if r == nil || r.images == nil {
		return 0
	}
	return r.images.Len(seeletelemetry.SessionIDFromContext(ctx))
}

// wrapImageAttachments 把待随图包装挂到最靠近 provider 的一层。
//
// 位置是有意的：它必须在 request log 包装**之内**（即先 log 再 wrap），
// 这样 request log 记录下来的就是真正发出去的消息——包含图片的那条。
// Stream / StreamEvents 以类型断言可选挂载：底层流式能力缺失时，Wrapper 会
// 退化为「一次同步 Complete + 单块回调」，与引擎自身的回退语义一致。
func (r *Runtime) wrapImageAttachments() {
	if r == nil || r.completer == nil || r.images == nil {
		return
	}
	wrapper := &imageattach.Wrapper{
		Next:    r.completer,
		Pending: r.images,
		Options: imageattach.Options{SessionID: seeletelemetry.SessionIDFromContext},
	}
	if streamer, ok := r.streamer.(agent.StreamCompleter); ok {
		wrapper.Stream = streamer
	}
	if events, ok := r.streamer.(agent.StreamEventCompleter); ok {
		wrapper.StreamEvents = events
	}
	r.completer = wrapper
	if r.streamer != nil {
		r.streamer = wrapper
	}
}
