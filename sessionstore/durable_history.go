package sessionstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/RedHuang-0622/Seele/types"
)

// isSessionNotFound 判断后端「会话不存在」错误（JSON 后端的 manifest 缺失）。
// 会话恢复的语义是空历史，而不是失败。
func isSessionNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// DurableHistory 实现 seelectx.DurableHistory 契约，把 sessionstore.Router
// 适配为 Session 的持久化所有者。Session 每次 Chat 前 Load、结束后 Save；
// Reset 显式清空（对应旧 ClearHistory + store 清理）。
//
// 编排规则（plan.md §3.2）：
//   - Load  = ProviderHistory 全量读（Router.Load）
//   - Save  = ProviderHistory 原子写 + 会话状态 blob 持久化（SaveState 编排）
//   - Clear = Router.Delete 的 Reset 语义（显式、可失败）
//
// SessionRecord/TranscriptEvent/ToolResults 的持久化继续由 Router 的
// SaveCommit 负责；本适配器只编排 provider 消息与 state blob。
type DurableHistory struct {
	router     *Router
	sessionID  string
	stateStore *SessionContextStore
}

// NewDurableHistory 创建绑定到指定会话的持久化适配器。
// router 为 nil 时退化为内存态（Load/Save 返回空、Clear 无操作）。
func NewDurableHistory(router *Router, sessionID string) *DurableHistory {
	return &DurableHistory{router: router, sessionID: sessionID}
}

// SessionID 返回绑定的会话 ID。
func (d *DurableHistory) SessionID() string { return d.sessionID }

// Load 读取 ProviderHistory 全量消息。新会话（尚无存储）返回空历史而非
// 错误，保证 Session 每次 Chat 前的 Load 可以正常开始。
func (d *DurableHistory) Load(_ context.Context) ([]types.Message, error) {
	if d == nil || d.router == nil || d.sessionID == "" {
		return []types.Message{}, nil
	}
	messages, err := d.router.Load(d.sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			return []types.Message{}, nil
		}
		return nil, fmt.Errorf("durable history: load %q: %w", d.sessionID, err)
	}
	return messages, nil
}

// Save 编排 ProviderHistory 原子写；若存在会话上下文存储则同步持久化
// state blob（WriteState），保证「历史 + 上下文栈」在同一会话边界落盘。
func (d *DurableHistory) Save(ctx context.Context, messages []types.Message) error {
	if d == nil || d.router == nil || d.sessionID == "" {
		return nil
	}
	if err := d.router.Save(d.sessionID, messages); err != nil {
		return fmt.Errorf("durable history: save %q: %w", d.sessionID, err)
	}
	if d.stateStore != nil {
		if err := d.stateStore.Persist(ctx); err != nil {
			return fmt.Errorf("durable history: save state %q: %w", d.sessionID, err)
		}
	}
	return nil
}

// LoadEventTail 按 token 预算 + 最大轮数读取最新完整协议单元（滑动窗口读，
// plan.md §3.7.2）。轮 = completeEventUnits 单元：窗口内轮次原样保留进入
// provider 请求，窗口外轮次才允许被 Controller 压缩（§3.7.4）。
func (d *DurableHistory) LoadEventTail(_ context.Context, tokenBudget, maxUnits int) ([]Event, error) {
	if d == nil || d.router == nil || d.sessionID == "" {
		return []Event{}, nil
	}
	events, err := d.router.LoadEventTail(d.sessionID, tokenBudget, maxUnits)
	if err != nil {
		if isSessionNotFound(err) {
			return []Event{}, nil
		}
		return nil, fmt.Errorf("durable history: load event tail %q: %w", d.sessionID, err)
	}
	return events, nil
}

// Clear 是 Reset 语义：删除会话的 ProviderHistory 与状态 blob。
func (d *DurableHistory) Clear(_ context.Context) error {
	if d == nil || d.router == nil || d.sessionID == "" {
		return nil
	}
	if err := d.router.Delete(d.sessionID); err != nil {
		return fmt.Errorf("durable history: clear %q: %w", d.sessionID, err)
	}
	if d.stateStore != nil {
		d.stateStore.ResetCache()
	}
	return nil
}

// AttachStateStore 绑定会话上下文存储，使 Save 时同步编排 state blob。
func (d *DurableHistory) AttachStateStore(store *SessionContextStore) {
	d.stateStore = store
}
