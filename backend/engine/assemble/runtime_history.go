package assemble

// runtime_history.go 是**历史检索端口**的宿主面（R11-S6，契约 §2.3：`ports.go` 归零片的最后一段）。
// 检索本体（压缩栈语义索引 + 事件库数据源）在 `backend/engine/context/seelectx/search`，
// 本层只做装配：nil 接收者与"事件库未装配"两条判据、把会话上下文存储当栈源、拼出检索器。

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
	"github.com/RedHuang-0622/seelex/backend/multiagent/agentsession"
	"github.com/RedHuang-0622/seelex/store"
	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

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

func (r *Runtime) newPreparedHistory(sessionID string) agentsession.PreparedHistory {
	router := r.HistoryRouter()
	if router == nil {
		return nil
	}
	return store.NewDurableHistory(router, sessionID)
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
