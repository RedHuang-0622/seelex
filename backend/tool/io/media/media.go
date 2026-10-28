// Package media 拥有「会话媒体落盘 + 下一次请求随图」这一条链路。
//
// 为什么落在这一层：截屏 / 看图类工具要让模型真正看到画面，需要两件事同时
// 成立——① 字节落进**执行会话自己**的媒体分区（可溯源、可回读、跨会话不可见）；
// ② 画面挂到「下一次」请求上且**只挂一次**（`Take` 即清空，见
// [imageattach.Registry]）。两件事共用同一套会话归属解析，所以同处一个包；宿主
// 只提供两件只读事实（见 [Host]），拓扑位置（挂在 Completer 链的哪一层）由装配
// 器决定。
package media

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	bridgecomputer "github.com/RedHuang-0622/seelex/backend/tool/computer"
	"github.com/RedHuang-0622/seelex/backend/tool/io/imageattach"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

// Host 是媒体域向宿主（装配器）要的两件只读事实。
//
// 刻意只留两格：媒体域不需要知道宿主是谁、也不需要宿主的任何写入口——它只把
// 「会话存储路由」与「会话 → 项目」两件事问出来。宿主是 `*assemble.Runtime`。
type Host interface {
	WriteMediaWorkspace(context.Context, string, string, storeartifact.MediaItem) (storeartifact.MediaRef, error)
	ReadMediaWorkspace(context.Context, string, string, string) (storeartifact.MediaRef, []byte, error)
	// MediaPathWorkspace 给出某条已落盘媒体资产的真实文件位置（GUI「点开缩略图，
	// 用系统默认查看器看原图」）。只读事实：媒体布局归存储 owner，本域不拼路径。
	MediaPathWorkspace(context.Context, string, string, string) (string, error)
	// MediaMetaWorkspace 只读某条已落盘媒体资产的**元数据**（不读字节）：把
	// 输入框附件的那几个 ref 解析成「这条用户消息带了哪几张图」的展示元数据时用，
	// 免得为了写一行 transcript 把几 MB 的图都读进内存。
	MediaMetaWorkspace(context.Context, string, string, string) (storeartifact.MediaRef, error)
	// SessionProjectID 解析会话绑定的项目 id；未绑定返回 ""。
	SessionProjectID(sessionID string) string
}

// SessionMedia 是一个会话进程内**唯一一份**媒体面：待随图队列 + 媒体分区读写法。
//
// 零值不可用，用 [New] 构造。方法一律带 nil 防护：装配期允许它在写完后才接上
// （与既有 `Runtime` 的其它域件同款）。
type SessionMedia struct {
	host    Host
	pending *imageattach.Registry
}

// New 构造媒体面。pending 队列由本包自建：它是进程内的一次性状态，没有第二个
// 使用者（工具侧 Add、请求侧 Take 都经本类型）。
func New(host Host) *SessionMedia {
	return &SessionMedia{host: host, pending: imageattach.NewRegistry()}
}

// Attach 把一张画面挂进「下一次模型请求随图」队列。
//
// 这是截屏 / 看图类工具的落点：工具负责把图片落进会话媒体分区并给出 ref，
// 引擎侧不需要知道图片的存在——Completer 接缝会把它挂到下一次请求上，而且
// 只挂一次（见 imageattach.Registry 的 Take 语义），避免反复占用 token 与配额。
func (m *SessionMedia) Attach(ctx context.Context, attachment imageattach.Attachment) {
	m.attach(seeletelemetry.SessionIDFromContext(ctx), attachment)
}

// attach 是**唯一**的入队实现：三个入口（截图工具、用户拖拽/粘贴、诊断）只负责
// 把会话 ID 解析出来，队列只在这里被写一次。会话为空时按队列语义静默丢弃。
func (m *SessionMedia) attach(sessionID string, attachment imageattach.Attachment) {
	if m == nil || m.pending == nil {
		return
	}
	m.pending.Add(sessionID, attachment)
}

// PendingCount 返回当前会话还有多少张待随图（诊断与断言用）。
func (m *SessionMedia) PendingCount(ctx context.Context) int {
	if m == nil || m.pending == nil {
		return 0
	}
	return m.pending.Len(seeletelemetry.SessionIDFromContext(ctx))
}

// StoreMedia 把截图落进**执行会话自己**的媒体分区（截屏工具经
// `computer.Deps.StoreMedia` 调用）。
//
// 归属解析见 [SessionMedia.ProjectIDFor]：主代理按会话绑定，子代理/节点执行按节点
// 作用域的工作区——两条路径都不读 Router 的活跃写作用域，后台/并行会话因此
// 不会把画面写进另一个项目。会话缺失时直接报错：宁可不落盘，也不能把别人的
// 画面写进错误的会话目录。
func (m *SessionMedia) StoreMedia(ctx context.Context, asset bridgecomputer.MediaAsset) (bridgecomputer.StoredMedia, error) {
	sessionID := seeletelemetry.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return bridgecomputer.StoredMedia{}, errors.New("computer: 缺少会话上下文，截图无法落盘")
	}
	return m.storeMedia(ctx, sessionID, asset)
}

// storeMedia 是**唯一**的落盘实现：会话 ID 由调用方解析（截图工具从 ctx、
// 输入框附件从显式参数），归属解析与写入只有这一处。
func (m *SessionMedia) storeMedia(ctx context.Context, sessionID string, asset bridgecomputer.MediaAsset) (bridgecomputer.StoredMedia, error) {
	if m == nil || m.host == nil {
		return bridgecomputer.StoredMedia{}, errors.New("media: 会话存储未装配")
	}
	ref, err := m.host.WriteMediaWorkspace(ctx, m.ProjectIDFor(ctx, sessionID), sessionID, storeartifact.MediaItem{
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

// AttachPending 把刚刚看到的画面挂进「下一次模型请求随图」队列
// （截屏工具经 `computer.Deps.AttachImage` 调用）：复用 [SessionMedia.Attach] 这一条
// 入队路径，只是把"缺少会话上下文"从静默丢弃变成显式报错——截了却送不到模型，工具
// 必须让调用方知道。
//
// 会话由执行 ctx 决定：主代理的截图进主会话的队列，子代理的截图进它自己的
// 队列——画面永远送给真正执行这次动作的那个会话。
func (m *SessionMedia) AttachPending(ctx context.Context, image bridgecomputer.PendingImage) error {
	if strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx)) == "" {
		return errors.New("computer: 缺少会话上下文，画面无法随请求送入模型")
	}
	if m == nil || m.pending == nil {
		return errors.New("computer: 随图队列未装配")
	}
	m.Attach(ctx, imageattach.Attachment{
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

// LoadMedia 按 `media:<hash>` 从执行会话的媒体分区读回图片：imageattach
// 的 ref 兜底路径（附件只带引用时）。与 StoreMedia 同一套归属解析，因此
// 不会把别的会话/项目的画面取过来。
func (m *SessionMedia) LoadMedia(ctx context.Context, ref string) (types.FilePart, error) {
	sessionID := seeletelemetry.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return types.FilePart{}, errors.New("computer: 缺少会话上下文，无法按 ref 读取媒体")
	}
	if m == nil || m.host == nil {
		return types.FilePart{}, errors.New("computer: 会话存储未装配，无法按 ref 读取媒体")
	}
	item, data, err := m.host.ReadMediaWorkspace(ctx, m.ProjectIDFor(ctx, sessionID), sessionID, ref)
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

// ProjectIDFor 解析媒体资产的归属项目（store 的 projectID 与
// workspace ID 同域）：
//
//  1. 执行会话自己的绑定（主代理/后台主会话走这条）；
//  2. 节点作用域的工作区（子代理/Plan 节点执行：node 会话没有独立绑定，但
//     ctx 里带着节点自己的工作区，与节点记录持久化同域且并行安全）；
//  3. 都没有则回落默认项目 ""（与 store 的"未绑定 = 默认项目"一致）。
//
// 刻意不读 Router 的活跃写作用域、也不用 MainSessionID 兜底：并行子代理执行
// 期间视图可能已经切到别的会话，用它们解析会把截图写进另一个项目（子代理
// 记录持久化踩过同一个坑，见 `runtime_subagent_recovery.go` 的同款注释）。
func (m *SessionMedia) ProjectIDFor(ctx context.Context, sessionID string) string {
	if m != nil && m.host != nil {
		if projectID := m.host.SessionProjectID(sessionID); projectID != "" {
			return projectID
		}
	}
	if scope, ok := dto.SceneScopeFromContext(ctx); ok {
		if workspaceID := strings.TrimSpace(scope.WorkspaceID); workspaceID != "" {
			return workspaceID
		}
	}
	return ""
}

// Wrap 把待随图包装挂到给定的一对 Completer 上，**返回**挂好的一对。
//
// 位置不由本包决定：装配器把它挂在 request log 包装**之内**（即先 log 再 wrap），
// 这样 request log 记录下来的就是真正发出去的消息——包含图片的那条。本包只保证
// "同一份 pending 队列 + 同一个 ref 兜底读法"接上。
//
// Stream / StreamEvents 以类型断言可选挂载：底层流式能力缺失时，Wrapper 会
// 退化为「一次同步 Complete + 单块回调」，与引擎自身的回退语义一致。
func (m *SessionMedia) Wrap(next agent.Completer, streamer agent.StreamCompleter) (agent.Completer, agent.StreamCompleter) {
	if m == nil || next == nil || m.pending == nil {
		return next, streamer
	}
	wrapper := &imageattach.Wrapper{
		Next:    next,
		Pending: m.pending,
		Options: imageattach.Options{
			SessionID: seeletelemetry.SessionIDFromContext,
			// ref 兜底路径接真实媒体分区：附件只带 `media:<hash>` 时按会话
			// 读回字节（截屏工具默认直接带字节，这条是"只给引用"形态的保障）。
			Loader: m.LoadMedia,
		},
	}
	if streamer != nil {
		wrapper.Stream = streamer
		if events, ok := streamer.(agent.StreamEventCompleter); ok {
			wrapper.StreamEvents = events
		}
	}
	if streamer == nil {
		return wrapper, nil
	}
	return wrapper, wrapper
}
