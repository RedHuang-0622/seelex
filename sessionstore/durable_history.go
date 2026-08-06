package sessionstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sync"

	"github.com/RedHuang-0622/Seele/types"
)

// isSessionNotFound 判断后端「会话不存在」错误（JSON 后端 fs.ErrNotExist，
// SQL 后端 sql.ErrNoRows）。会话恢复的语义是空历史/空记录，而不是失败。
func isSessionNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows)
}

// GapCoverer 覆盖「滑动窗口与压缩内容之间」的真空区：事件流中最后一次
// 压缩（CompactStack 顶帧 To）之后、本次尾窗装载之前、从未被压缩的轮次，
// 既不在窗口也不在任何压缩帧——冷启动/恢复时若不覆盖即从请求中永久丢失。
//
// allEvents 是会话完整事件流（append-only，含已压缩轮次；未经尾窗选择），
// tailEvents 是本次 Load 实际装载进 provider 请求的窗口单元事件。实现通常
// 把真空区轮次压缩为合并帧（PushCompact 到 state store）并可选归档原文；
// 返回 error 时 Load 保守忽略（不因覆盖失败阻断请求）。
type GapCoverer func(ctx context.Context, allEvents, tailEvents []Event) error

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
	router      *Router
	sessionID   string
	stateStore  *SessionContextStore
	mu          sync.Mutex
	prepared    []types.Message
	preparedSet bool
	tail        *historyTailBudget // 滑动窗口读尾预算（nil = 全量加载，旧语义）
	gapCoverer  GapCoverer         // 真空区覆盖回调（nil = 不覆盖）
}

// historyTailBudget 是 Load 的滑动窗口读尾预算（token + 轮数；0 = 不限）。
type historyTailBudget struct {
	tokenBudget int
	maxUnits    int
}

// NewDurableHistory 创建绑定到指定会话的持久化适配器。
// router 为 nil 时退化为内存态（Load/Save 返回空、Clear 无操作）。
func NewDurableHistory(router *Router, sessionID string) *DurableHistory {
	return &DurableHistory{router: router, sessionID: sessionID}
}

// SetTailBudget 注入滑动窗口读尾预算（plan.md §3.7.2：Load 只装载窗口
// 区间——token + 轮数双上限；窗口外轮次由 CompactStack 摘要承接）。
// 未调用（nil）→ Load 保持全量（旧语义，兼容未接线的调用方）。
func (d *DurableHistory) SetTailBudget(tokenBudget, maxUnits int) {
	d.tail = &historyTailBudget{tokenBudget: tokenBudget, maxUnits: maxUnits}
}

// SetGapCoverer 注入真空区覆盖回调：每次尾窗 Load 时携带完整事件流与
// 实际装载的窗口事件调用；回调负责把「压缩点之后、窗口之前」的未压缩
// 轮次压缩进 CompactStack（不修改本适配器返回的历史）。
func (d *DurableHistory) SetGapCoverer(coverer GapCoverer) {
	d.gapCoverer = coverer
}

// SessionID 返回绑定的会话 ID。
func (d *DurableHistory) SessionID() string { return d.sessionID }

// PrepareNextLoad hands the application-assembled bounded history to the
// framework's next ChatStream restore. It is consumed exactly once so a
// subsequent request cold-loads the durable owner normally.
func (d *DurableHistory) PrepareNextLoad(messages []types.Message) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.prepared = append([]types.Message(nil), messages...)
	d.preparedSet = true
	d.mu.Unlock()
}

// Load 读取 ProviderHistory 消息。配置了窗口预算（SetTailBudget）时按
// token + 轮数读尾（滑动窗口加载区间，D1：只装载窗口，不拉全量）；
// 未配置 → 全量（旧语义）。新会话（尚无存储）返回空历史而非错误，
// 保证 Session 每次 Chat 前的 Load 可以正常开始。
func (d *DurableHistory) Load(ctx context.Context) ([]types.Message, error) {
	if d == nil {
		return []types.Message{}, nil
	}
	d.mu.Lock()
	if d.preparedSet {
		prepared := append([]types.Message(nil), d.prepared...)
		d.prepared = nil
		d.preparedSet = false
		d.mu.Unlock()
		return prepared, nil
	}
	d.mu.Unlock()
	if d.router == nil || d.sessionID == "" {
		return []types.Message{}, nil
	}
	if d.tail != nil && (d.tail.tokenBudget > 0 || d.tail.maxUnits > 0) {
		// 一次全读 + 进程内尾窗选择：ReadEventTail 各后端本就全读后选择，
		// 这里显式全读以便真空区覆盖回调拿到完整事件流（含已压缩轮次）。
		allEvents, err := d.loadAllEvents(ctx)
		if err != nil {
			return nil, err
		}
		tail := selectEventTail(allEvents, d.tail.tokenBudget, d.tail.maxUnits)
		if d.gapCoverer != nil && len(allEvents) > 0 {
			if err := d.gapCoverer(ctx, allEvents, tail); err != nil {
				// 覆盖失败保守忽略：窗口历史已可继续请求，真空区下次 Load
				// 重试；不因上下文增强失败阻断 Chat。
				_ = err
			}
		}
		return eventsToMessages(tail), nil
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

// eventsToMessages 把完整协议单元事件流转为 types.Message（窗口读尾的
// 载荷转换；事件已保证轮次完整性，直接映射字段）。
func eventsToMessages(events []Event) []types.Message {
	if len(events) == 0 {
		return []types.Message{}
	}
	messages := make([]types.Message, 0, len(events))
	for _, event := range events {
		message := types.Message{
			Role:             event.Role,
			ReasoningContent: event.ReasoningContent,
			Content:          strPtrOrNil(event.Content),
			ToolCallID:       event.ToolCallID,
			Name:             event.Name,
		}
		for _, call := range event.ToolCalls {
			message.ToolCalls = append(message.ToolCalls, types.ToolCall{
				ID: call.ID, Type: "function", Function: types.ToolCallFunction{
					Name: call.Name, Arguments: call.Arguments,
				},
			})
		}
		messages = append(messages, message)
	}
	return messages
}

func strPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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

// loadAllEvents 读取会话完整事件流（MaxInt 双限 = selectEventTail 无截断，
// 返回全部完整协议单元事件；非单元事件不构成 provider 上下文）。
func (d *DurableHistory) loadAllEvents(ctx context.Context) ([]Event, error) {
	return d.LoadEventTail(ctx, math.MaxInt, math.MaxInt)
}

// LoadEventTail 按 token 预算 + 最大轮数读取最新完整协议单元（滑动窗口读，
// plan.md §3.7.2）。轮 = CompleteEventUnits 单元：窗口内轮次原样保留进入
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
	if d == nil {
		return nil
	}
	d.mu.Lock()
	d.prepared = nil
	d.preparedSet = false
	d.mu.Unlock()
	if d.router == nil || d.sessionID == "" {
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
