package sessionstore

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/lifecycle"
)

// RouterStorage 把 sessionstore.Router 适配为 lifecycle.Storage[types.Message]
// （docs/2026-08-04-context-memory-lifecycle/plan.md §2.2 基建-A）：
// ContextActor 以 Router 为冷存储的接缝——Append 事件追加落库、
// ReadRange 按偏移窗口读、Count 读总数。
//
// 依赖方向：sessionstore → seelexctx/lifecycle（lifecycle 保持通用，不反向
// 依赖任何存储实现）。
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

// 编译期断言：RouterStorage 实现 lifecycle.Storage[types.Message]。
var _ lifecycle.Storage[types.Message] = (*RouterStorage)(nil)
