package assemble

// runtime_history.go 是**历史检索端口**的宿主面（R11-S6，契约 §2.3：`ports.go` 归零片的最后一段）。
// 检索本体（压缩栈语义索引 + 事件库数据源）在 `backend/engine/context/seelectx/search`，
// 本层只做装配：nil 接收者与"事件库未装配"两条判据、把会话上下文存储当栈源、拼出检索器。

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
)

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
	searcher := search.New(stack, search.NewRouterEventSource(router, router.Workspace(), r.MainSessionID()))
	return searcher.Search(ctx, query, search.Options{Limit: limit})
}
