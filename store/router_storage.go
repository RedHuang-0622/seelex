package store

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/types"
)

// RouterStorage 把 store.Router 适配为「Append / ReadRange / Count」这一冷存储形状
// （docs/2026-08-04-context-memory-lifecycle/plan.md §2.2 基建-A）：
// ContextActor 以 Router 为冷存储的接缝——Append 事件追加落库、
// ReadRange 按偏移窗口读、Count 读总数。
//
// 依赖方向（M3-S4 修正）：本文件**不再** import `seelexctx/lifecycle`——那是底座
// 唯一一条向上依赖（`store → seelexctx`），会让 store 反向依赖上层。方法集
// 本身仍与 `lifecycle.Storage[types.Message]` 同形，但「它满足那个接口」的编译期
// 断言钉在**消费侧**：`seelexctx/router_storage_conformance.go`。
//
// 为什么不用「契约层通用端口 + 结构满足」（设计稿 A §2.4 方案 a）：`contract/ports.go`
// 现在 import `seelexctx/search`（D-M1-1），而 `seelexctx/search` import `store`
// ⇒ `store → contract` 会立刻构成 import 环。契约层那条污染收口后可改回契约
// 端口路线（见 m3-record.md 的 D-M3-11）。
type RouterStorage struct {
	router    *Router
	sessionID string
}

// NewRouterStorage 构造 Router 冷存储适配器（绑定会话）。
func NewRouterStorage(router *Router, sessionID string) *RouterStorage {
	return &RouterStorage{router: router, sessionID: sessionID}
}

// Append 把事件行追加进 message 通道（② 后端写路径）。v8 JSON 布局以
// message 事件行为唯一正文事实源（D9/S11：history.json 整段替换缓存已
// 退役），不再读-合并-整段写；读面 LoadRange 由同一行派生——追加 → 可读
// → 可计数。
func (s *RouterStorage) Append(ctx context.Context, messages []types.Message) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return nil
	}
	if len(messages) == 0 {
		return nil
	}
	rows := messagesToEventRows(messages)
	return s.router.SaveCommit(s.sessionID, Commit{Events: rows})
}

// ReadRange 按 [offset, offset+limit) 读消息，返回区间与总数
// （① 前端 select / ③ 递 LLM 的窗口读路径）。
func (s *RouterStorage) ReadRange(ctx context.Context, offset, limit int) ([]types.Message, int, error) {
	if s == nil || s.router == nil || s.sessionID == "" {
		return []types.Message{}, 0, nil
	}
	messages, total, err := s.router.LoadRange(s.sessionID, offset, limit)
	if err != nil {
		if isSessionNotFound(err) {
			return []types.Message{}, 0, nil
		}
		return nil, 0, fmt.Errorf("router storage: read range %q: %w", s.sessionID, err)
	}
	return messages, total, nil
}

// Count 返回已落库消息总数。
func (s *RouterStorage) Count() int {
	if s == nil || s.router == nil || s.sessionID == "" {
		return 0
	}
	_, total, err := s.router.LoadRange(s.sessionID, 0, 0)
	if err != nil {
		return 0
	}
	return total
}
