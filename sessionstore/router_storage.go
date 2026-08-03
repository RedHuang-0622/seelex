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

// Append 读-合并-写落库（② 后端写路径）。基建期 O(n)：Router.Save 是
// 全量原子写（消息 blob 面），读面 LoadRange 读同一面——语义一致可读。
// 增量优化（事件库 SaveCommit + 投影）为后续切片；当前保证 lifecycle
// Storage 接口语义完整（追加 → 可读 → 可计数）。
func (s *RouterStorage) Append(ctx context.Context, messages []types.Message) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return nil
	}
	// limit<=0 的 LoadRange 只返回 total 不返回内容——先取总数再读全量。
	_, total, err := s.router.LoadRange(s.sessionID, 0, 0)
	if err != nil && !isSessionNotFound(err) {
		return fmt.Errorf("router storage: read for merge %q: %w", s.sessionID, err)
	}
	var existing []types.Message
	if total > 0 {
		existing, _, err = s.router.LoadRange(s.sessionID, 0, total)
		if err != nil && !isSessionNotFound(err) {
			return fmt.Errorf("router storage: read for merge %q: %w", s.sessionID, err)
		}
	}
	merged := existing
	if len(messages) > 0 {
		merged = append(existing, messages...)
	}
	return s.router.Save(s.sessionID, merged)
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
