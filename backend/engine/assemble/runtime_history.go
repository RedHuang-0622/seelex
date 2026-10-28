package assemble

// runtime_history.go 是**历史检索端口**的宿主面（R11-S6，契约 §2.3：`ports.go` 归零片的最后一段）。
// 检索本体（压缩栈语义索引 + 事件库数据源）在 `backend/engine/context/seelectx/search`，
// 本层只做装配：nil 接收者与"事件库未装配"两条判据、把会话上下文存储当栈源、拼出检索器。

import (
	"context"
	"errors"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
	"github.com/RedHuang-0622/seelex/backend/engine/obs"
	"github.com/RedHuang-0622/seelex/backend/multiagent/agentsession"
	toolmedia "github.com/RedHuang-0622/seelex/backend/tool/io/media"
	"github.com/RedHuang-0622/seelex/store"
	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

type durableHistoryRouter = store.Router

type mediaStoreState struct{ store *storeartifact.SessionStore }

// AttachHistoryRouter installs the provider-history plane independently from
// SessionContextStore, whose state blob has a different owner.
func (r *Runtime) AttachHistoryRouter(router *store.Router) {
	r.historyRouterMu.Lock()
	r.historyRouter = router
	r.historyRouterMu.Unlock()
}

// HistoryRouter returns the session store route (nil before assembly).
func (r *Runtime) HistoryRouter() *store.Router {
	r.historyRouterMu.RLock()
	defer r.historyRouterMu.RUnlock()
	return r.historyRouter
}

func (r *Runtime) unifiedEventStore() (*store.EventStore, error) {
	router := r.HistoryRouter()
	if router == nil {
		return nil, obs.ErrUnifiedEventsRouterMissing
	}
	return store.NewEventStore(router), nil
}

func (r *Runtime) loadUnifiedEvents(ctx context.Context, sessionID string) ([]frameworkevent.Event, error) {
	eventStore, err := r.unifiedEventStore()
	if err != nil {
		return nil, err
	}
	return eventStore.Load(ctx, sessionID)
}

func (r *Runtime) loadUnifiedEventRange(ctx context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error) {
	eventStore, err := r.unifiedEventStore()
	if err != nil {
		return nil, err
	}
	return eventStore.LoadRange(ctx, sessionID, fromSeq, toSeq)
}

func (r *Runtime) newPreparedHistory(sessionID string) agentsession.PreparedHistory {
	router := r.HistoryRouter()
	if router == nil {
		return nil
	}
	return store.NewDurableHistory(router, sessionID)
}

func (r *Runtime) newConfiguredMainHistory(sessionID string) agentsession.PreparedHistory {
	router := r.HistoryRouter()
	if router == nil {
		return nil
	}
	history := store.NewDurableHistory(router, sessionID)
	history.SetWorkspaceResolver(r.sessionWorkspaceFor(sessionID))
	history.SetTailBudget(r.windowTailBudget())
	// 真空区覆盖：滑动窗口与压缩内容之间未压缩轮次的断档填补
	// （seelexctx/gap.go；压缩只随活跃期事件触发，会话结束后窗口外的
	// 未压缩轮次会被尾窗装载丢弃——Load 时按尾窗起始单元下标，以完整
	// 事件流补压合并帧）。
	history.SetGapCoverer(r.coverHistoryGap)
	return history
}

// WriteMediaWorkspace 是 media 域的宿主窄端口。
func (r *Runtime) WriteMediaWorkspace(ctx context.Context, projectID, sessionID string, item storeartifact.MediaItem) (storeartifact.MediaRef, error) {
	if r == nil {
		return storeartifact.MediaRef{}, errors.New("media: runtime is unavailable")
	}
	state := r.mediaStore.Load()
	if state == nil || state.store == nil {
		return storeartifact.MediaRef{}, errors.New("media: artifact store is unavailable")
	}
	return state.store.WriteMedia(ctx, projectID, sessionID, item)
}

// ReadMediaWorkspace 是 media 域的宿主窄端口。
func (r *Runtime) ReadMediaWorkspace(ctx context.Context, projectID, sessionID, ref string) (storeartifact.MediaRef, []byte, error) {
	if r == nil {
		return storeartifact.MediaRef{}, nil, errors.New("media: runtime is unavailable")
	}
	state := r.mediaStore.Load()
	if state == nil || state.store == nil {
		return storeartifact.MediaRef{}, nil, errors.New("media: artifact store is unavailable")
	}
	return state.store.ReadMedia(ctx, projectID, sessionID, ref)
}

// MediaPathWorkspace 是 media 域的宿主窄端口（GUI「点开缩略图」要真实文件位置）。
func (r *Runtime) MediaPathWorkspace(ctx context.Context, projectID, sessionID, ref string) (string, error) {
	if r == nil {
		return "", errors.New("media: runtime is unavailable")
	}
	state := r.mediaStore.Load()
	if state == nil || state.store == nil {
		return "", errors.New("media: artifact store is unavailable")
	}
	return state.store.MediaPath(ctx, projectID, sessionID, ref)
}

// MediaMetaWorkspace 是 media 域的宿主窄端口：只读一条媒体资产的元数据。
//
// 为什么要与 ReadMediaWorkspace 分开：提交带图消息时只需要"这张图叫什么、多大"
// 去写一行 transcript，图本体可能有数 MB——为了元数据把字节也读进来是白花。
func (r *Runtime) MediaMetaWorkspace(ctx context.Context, projectID, sessionID, ref string) (storeartifact.MediaRef, error) {
	if r == nil {
		return storeartifact.MediaRef{}, errors.New("media: runtime is unavailable")
	}
	state := r.mediaStore.Load()
	if state == nil || state.store == nil {
		return storeartifact.MediaRef{}, errors.New("media: artifact store is unavailable")
	}
	return state.store.MediaMeta(ctx, projectID, sessionID, ref)
}

// ComposerMedia 把媒体面交给 GUI 装配（输入框图片附件的唯一 owner）。
//
// 为什么走这个 getter 而不是把 `*toolmedia.SessionMedia` 塞进
// `application/assembly.Components`：媒体域属于 backend，应用层不该认识它；桌面宿主
// 是组合根，它手里已经有 Runtime，取一格窄端口就够——端口形状由消费方（gui）声明，
// 这里的返回值只是"结构上满足"，赋值语句本身就是编译期断言。
func (r *Runtime) ComposerMedia() *toolmedia.SessionMedia {
	if r == nil {
		return nil
	}
	return r.media
}

// AttachMediaStore injects the artifact-owned session-scoped media entry.
func (r *Runtime) AttachMediaStore(store *storeartifact.SessionStore) {
	if r == nil {
		return
	}
	r.mediaStore.Store(&mediaStoreState{store: store})
}

// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录（GUI 历史检索
// 面板与 search_history 工具共享的数据面；query 非空校验由调用方/检索器
// 双层执行，limit 为 token 预算在 search 包内 clamp 到硬上限）。
func (r *Runtime) SearchHistory(ctx context.Context, query string, limit int) (search.Result, error) {
	if r == nil {
		return search.Result{}, errors.New("search_history: runtime is unavailable")
	}
	router := r.HistoryRouter()
	if router == nil {
		return search.Result{}, errors.New("search_history: 事件库未装配（会话持久化未启用）")
	}
	var stack search.StackSource
	if store := r.sessionContextStore(); store != nil {
		stack = store
	}
	searcher := search.New(stack, search.NewRouterEventSource(router, router.Workspace(), r.MainSessionID(), store.IsSessionNotFound))
	return searcher.Search(ctx, query, search.Options{Limit: limit})
}
