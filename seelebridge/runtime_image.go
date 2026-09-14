package seelebridge

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/imageattach"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	bridgecomputer "github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
	"github.com/RedHuang-0622/seelex/sessionstore"
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

// storeSessionMedia 把截图落进**执行会话自己**的媒体分区（截屏工具经
// Deps.StoreMedia 调用）。
//
// 归属解析见 mediaProjectIDFor：主代理按会话绑定，子代理/节点执行按节点
// 作用域的工作区——两条路径都不读 Router 的活跃写作用域，后台/并行会话因此
// 不会把画面写进另一个项目。会话缺失时直接报错：宁可不落盘，也不能把别人的
// 画面写进错误的会话目录。
func (r *Runtime) storeSessionMedia(ctx context.Context, asset bridgecomputer.MediaAsset) (bridgecomputer.StoredMedia, error) {
	sessionID := seeletelemetry.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return bridgecomputer.StoredMedia{}, errors.New("computer: 缺少会话上下文，截图无法落盘")
	}
	router := r.durableHistoryRouter()
	if router == nil {
		return bridgecomputer.StoredMedia{}, errors.New("computer: 会话存储未装配，截图无法落盘")
	}
	ref, err := router.WriteMediaWorkspace(ctx, r.mediaProjectIDFor(ctx, sessionID), sessionID, sessionstore.MediaItem{
		Name:     asset.Name,
		MimeType: asset.MimeType,
		Kind:     asset.Kind,
		Data:     asset.Data,
		Width:    asset.Width,
		Height:   asset.Height,
		Scale:    asset.Scale,
	})
	if err != nil {
		return bridgecomputer.StoredMedia{}, err
	}
	return bridgecomputer.StoredMedia{Ref: ref.Ref, Hash: ref.Hash, Bytes: ref.Bytes, Name: ref.Name}, nil
}

// attachSessionImage 把刚刚看到的画面挂进「下一次模型请求随图」队列
// （截屏工具经 Deps.AttachImage 调用）：复用 AttachImage 这一条入队路径，
// 只是把"缺少会话上下文"从静默丢弃变成显式报错——截了却送不到模型，工具
// 必须让调用方知道。
//
// 会话由执行 ctx 决定：主代理的截图进主会话的队列，子代理的截图进它自己的
// 队列——画面永远送给真正执行这次动作的那个会话。
func (r *Runtime) attachSessionImage(ctx context.Context, image bridgecomputer.PendingImage) error {
	if strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx)) == "" {
		return errors.New("computer: 缺少会话上下文，画面无法随请求送入模型")
	}
	if r.images == nil {
		return errors.New("computer: 随图队列未装配")
	}
	r.AttachImage(ctx, imageattach.Attachment{
		Ref:   image.Ref,
		Label: image.Label,
		File: types.FilePart{
			Kind:     types.FileKindImage,
			MimeType: image.MimeType,
			Data:     image.Data,
			Width:    image.Width,
			Height:   image.Height,
			Name:     image.Name,
		},
	})
	return nil
}

// loadSessionMedia 按 `media:<hash>` 从执行会话的媒体分区读回图片：imageattach
// 的 ref 兜底路径（附件只带引用时）。与 storeSessionMedia 同一套归属解析，因此
// 不会把别的会话/项目的画面取过来。
func (r *Runtime) loadSessionMedia(ctx context.Context, ref string) (types.FilePart, error) {
	sessionID := seeletelemetry.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return types.FilePart{}, errors.New("computer: 缺少会话上下文，无法按 ref 读取媒体")
	}
	router := r.durableHistoryRouter()
	if router == nil {
		return types.FilePart{}, errors.New("computer: 会话存储未装配，无法按 ref 读取媒体")
	}
	item, data, err := router.ReadMediaWorkspace(ctx, r.mediaProjectIDFor(ctx, sessionID), sessionID, ref)
	if err != nil {
		return types.FilePart{}, err
	}
	kind := types.FileKindImage
	if !strings.HasPrefix(strings.ToLower(item.MimeType), "image/") {
		kind = types.FileKindDocument
	}
	return types.FilePart{
		Kind:     kind,
		MimeType: item.MimeType,
		Data:     data,
		Width:    item.Width,
		Height:   item.Height,
		Name:     item.Name,
	}, nil
}

// mediaProjectIDFor 解析媒体资产的归属项目（sessionstore 的 projectID 与
// workspace ID 同域）：
//
//  1. 执行会话自己的绑定（主代理/后台主会话走这条）；
//  2. 节点作用域的工作区（子代理/Plan 节点执行：node 会话没有独立绑定，但
//     ctx 里带着节点自己的工作区，与节点记录持久化同域且并行安全）；
//  3. 都没有则回落默认项目 ""（与 sessionstore 的"未绑定 = 默认项目"一致）。
//
// 刻意不读 Router 的活跃写作用域、也不用 MainSessionID 兜底：并行子代理执行
// 期间视图可能已经切到别的会话，用它们解析会把截图写进另一个项目（子代理
// 记录持久化踩过同一个坑，见 runtime_subagent_recovery.go 的同款注释）。
func (r *Runtime) mediaProjectIDFor(ctx context.Context, sessionID string) string {
	if projectID := r.sessionProjectIDFor(sessionID); projectID != "" {
		return projectID
	}
	if scope, ok := model.NodeScopeFromContext(ctx); ok {
		if workspaceID := strings.TrimSpace(scope.WorkspaceID); workspaceID != "" {
			return workspaceID
		}
	}
	return ""
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
		Options: imageattach.Options{
			SessionID: seeletelemetry.SessionIDFromContext,
			// ref 兜底路径接真实媒体分区：附件只带 `media:<hash>` 时按会话
			// 读回字节（截屏工具默认直接带字节，这条是"只给引用"形态的保障）。
			Loader: r.loadSessionMedia,
		},
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
