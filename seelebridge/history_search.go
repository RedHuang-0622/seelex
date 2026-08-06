// 长上下文历史记录检索（2026-08-07）：压缩栈帧是语义索引，检索在其
// 范围内查真实聊天记录。
//
// 链路：Runtime 持有会话上下文存储（CompactStack，state blob）与历史路由
// （事件库，append-only 真相源）——SearchHistory 把两者接线到
// seelexctx/search.Searcher：memory.Select 在全部压缩帧上选相关帧 → 按帧
// [From..To] 单元范围从事件库读回真实记录 → token 预算内有界返回。
// 无压缩栈 → 尾部扫描兜底（短会话可检索）；事件库未装配（无持久化
// Router）→ 显式错误（检索不可用）。
package seelebridge

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/seelexctx/search"
)

// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录（GUI 历史检索
// 面板与 search_history 工具共享的数据面；query 非空校验由调用方/检索器
// 双层执行，limit 与 token 预算在 search 包内 clamp 到硬上限）。
func (r *Runtime) SearchHistory(ctx context.Context, query string, limit int) (search.Result, error) {
	if r == nil {
		return search.Result{}, errors.New("search_history: runtime is unavailable")
	}
	router := r.durableHistoryRouter()
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
