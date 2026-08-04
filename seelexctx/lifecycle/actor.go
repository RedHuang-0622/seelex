// Package lifecycle 提供上下文生命周期管理的泛型 actor 模板
// （docs/2026-08-04-context-memory-lifecycle/plan.md §2.2）。
//
// Actor 语义（与 filesystem actor 和 Runtime mailbox 同构）：
// 状态私有——唯一 actor goroutine 持有全部上下文状态，业务代码零 mutex；
// 消息进出——外部经有界 mailbox（channel）投递操作，回复经 reply channel。
// 并发友好：多 goroutine 可同时 Append/LoadWindow，actor 串行消费保序。
//
// 生命周期策略（LifecyclePolicy）注入决定内存驻留行为：
//   - FullRetain：全量常驻（基准对照组）；
//   - ColdLoad：不驻留，读时经 Storage 冷加载（目标策略）；
//   - Windowed：驻留滑动窗口（窗口外落库释放）；
//   - Pipelined：写走批量管道（BatchPipeline 聚合落库）。
package lifecycle

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Storage 是上下文的冷存储面（唯一持久面）。
// 实现必须并发安全（actor 之外可能被管道/测试直连）。
type Storage[T any] interface {
	// Append 批量追加（落库）。
	Append(ctx context.Context, items []T) error
	// ReadRange 按 [offset, offset+limit) 读，返回区间与总数。
	ReadRange(ctx context.Context, offset, limit int) ([]T, int, error)
	// Count 返回已落库条数。
	Count() int
}

// LifecyclePolicy 是 actor 的内存驻留策略。
type LifecyclePolicy int

const (
	// PolicyFullRetain 全量常驻（基准对照组；长会话下内存无界）。
	PolicyFullRetain LifecyclePolicy = iota
	// PolicyColdLoad 冷加载：Append 即落库并释放，读时经 Storage 装载。
	PolicyColdLoad
	// PolicyWindowed 驻留滑动窗口：保留最近 N 条，窗口外落库释放。
	PolicyWindowed
	// PolicyPipelined 管道批量：Append 经 BatchPipeline 聚合落库。
	PolicyPipelined
)

func (p LifecyclePolicy) String() string {
	switch p {
	case PolicyFullRetain:
		return "full-retain"
	case PolicyColdLoad:
		return "cold-load"
	case PolicyWindowed:
		return "windowed"
	case PolicyPipelined:
		return "pipelined"
	default:
		return fmt.Sprintf("policy(%d)", int(p))
	}
}

// op 是 actor 消息的操作码。
type op int

const (
	opAppend op = iota
	opLoadWindow
	opSnapshot
	opClose
)

// reply 是 actor 回复（容量 1 的 channel，非阻塞回传）。
type reply[T any] struct {
	items []T
	total int
	err   error
}

// request 是 actor 消息载荷（值对象，复制进出）。
type request[T any] struct {
	op     op
	items  []T
	offset int
	limit  int
	reply  chan reply[T]
}

// ContextActor 是上下文生命周期 actor（泛型，无锁闭包状态）。
// 外部只能经 Enqueue/Append/LoadWindow/Snapshot 发消息，绝不直接访问内部。
type ContextActor[T any] struct {
	mailbox    chan request[T]
	ctx        context.Context
	cancel     context.CancelFunc
	closed     chan struct{}
	closedFlag atomic.Bool // Close 开始时置位（Enqueue 显式拒绝）
	closeOnce  sync.Once
	gate       sync.RWMutex // 关闭 mailbox 与并发投递之间的生命周期门

	// 以下字段仅 actor goroutine 访问（闭包状态，无锁）。
	store     Storage[T]
	policy    LifecyclePolicy
	window    int // PolicyWindowed 的驻留窗口条数
	resident  []T // 常驻区（FullRetain 全量 / Windowed 窗口 / ColdLoad 空）
	stored    int
	seq       int          // 已接收消息序号（保序审计）
	drop      atomic.Int64 // 背压丢弃计数（非阻塞投递满时；跨 goroutine 原子）
	opTimeout time.Duration
}

// Options 是 actor 构造参数。
type Options struct {
	// MailboxSize 是消息队列容量（默认 256；0 = 默认）。
	MailboxSize int
	// Window 是 PolicyWindowed 的驻留条数（0 = 默认 512）。
	Window           int
	OperationTimeout time.Duration
}

// NewContextActor 构造上下文 actor：启动消息循环（actor goroutine）。
// store 为 nil 时使用内存存储（mock/基准场景）。
func NewContextActor[T any](policy LifecyclePolicy, store Storage[T], options Options) *ContextActor[T] {
	mailboxSize := options.MailboxSize
	if mailboxSize <= 0 {
		mailboxSize = 256
	}
	window := options.Window
	if window <= 0 {
		window = 512
	}
	opTimeout := options.OperationTimeout
	if opTimeout <= 0 {
		opTimeout = 5 * time.Second
	}
	if store == nil {
		store = newMemoryStorage[T]()
	}
	ctx, cancel := context.WithCancel(context.Background())
	actor := &ContextActor[T]{
		mailbox:   make(chan request[T], mailboxSize),
		ctx:       ctx,
		cancel:    cancel,
		closed:    make(chan struct{}),
		store:     store,
		policy:    policy,
		window:    window,
		opTimeout: opTimeout,
	}
	go actor.run()
	return actor
}

// run 是唯一状态持有者：串行消费 mailbox（无锁闭包状态）。
func (a *ContextActor[T]) run() {
	defer close(a.closed)
	for req := range a.mailbox {
		a.handle(req)
	}
}

// handle 分发消息（actor goroutine 内）。
func (a *ContextActor[T]) handle(req request[T]) {
	switch req.op {
	case opAppend:
		a.handleAppend(req)
	case opLoadWindow:
		a.handleLoadWindow(req)
	case opSnapshot:
		a.handleSnapshot(req)
	case opClose:
		// 关闭由调用方 cancel 触发；此处仅应答。
		if req.reply != nil {
			req.reply <- reply[T]{total: a.seq}
		}
	}
	a.seq++
}

// handleAppend 写路径：按策略决定驻留 vs 落库。
func (a *ContextActor[T]) handleAppend(req request[T]) {
	switch a.policy {
	case PolicyFullRetain:
		a.resident = append(a.resident, req.items...)
	case PolicyColdLoad:
		// 冷加载：立即落库，不驻留（写路径内存只短暂持有消息副本）。
		if err := a.appendToStore(req.items); err != nil {
			a.replyErr(req, err)
			return
		}
	case PolicyWindowed:
		a.resident = append(a.resident, req.items...)
		if len(a.resident) > a.window {
			// 窗口外落库释放（只保留最近 window 条）。截断用精确 cap
			// 复制（append 到 nil 的 cap 有增长策略，会保留多余容量）。
			overflow := a.resident[:len(a.resident)-a.window]
			if err := a.appendToStore(overflow); err != nil {
				a.replyErr(req, err)
				return
			}
			overflow = nil // 断引用，旧数组可回收
			trimmed := make([]T, a.window)
			copy(trimmed, a.resident[len(a.resident)-a.window:])
			a.resident = trimmed
		}
	case PolicyPipelined:
		// 管道批量由外部 BatchPipeline 聚合后调用 Append（此处等同落库，
		// 不驻留；聚合在管道层完成）。
		if err := a.appendToStore(req.items); err != nil {
			a.replyErr(req, err)
			return
		}
	}
	if req.reply != nil {
		req.reply <- reply[T]{total: len(a.resident) + a.stored}
	}
}

// handleLoadWindow 读路径（① 前端 select / ③ 递 LLM）：
// 窗口读优先常驻区，窗口外从 Storage 冷加载。
func (a *ContextActor[T]) handleLoadWindow(req request[T]) {
	total := len(a.resident) + a.stored
	// 空存储 + offset 0 → 空区间（合法，不报错）；负 offset / 越界才报错。
	if req.offset < 0 || (total > 0 && req.offset >= total) {
		if req.reply != nil {
			req.reply <- reply[T]{items: []T{}, total: total, err: fmt.Errorf("lifecycle: offset %d out of range [0,%d)", req.offset, total)}
		}
		return
	}
	if req.limit <= 0 {
		if req.reply != nil {
			req.reply <- reply[T]{items: []T{}, total: total}
		}
		return
	}
	// 常驻区覆盖（尾部）；窗口前段从冷存储读。
	var items []T
	residentStart := total - len(a.resident)
	if req.offset >= residentStart {
		// 全部在常驻区。
		start := req.offset - residentStart
		end := min(start+req.limit, len(a.resident))
		items = append([]T(nil), a.resident[start:end]...)
	} else {
		// 冷存储段 + 常驻段拼接。
		ctx, cancel := a.operationContext()
		fromStore, _, err := a.store.ReadRange(ctx, req.offset, req.limit)
		cancel()
		if err != nil {
			a.replyErr(req, err)
			return
		}
		items = append(items, fromStore...)
		if len(items) < req.limit {
			start := 0
			end := min(req.limit-len(items), len(a.resident))
			items = append(items, a.resident[start:end]...)
		}
	}
	if req.reply != nil {
		req.reply <- reply[T]{items: items, total: total}
	}
}

// handleSnapshot 返回当前常驻区与总数（审计/测试）。
func (a *ContextActor[T]) handleSnapshot(req request[T]) {
	if req.reply != nil {
		req.reply <- reply[T]{items: append([]T(nil), a.resident...), total: len(a.resident) + a.stored}
	}
}

// replyErr 回传错误（不中断 actor 循环）。
func (a *ContextActor[T]) replyErr(req request[T], err error) {
	if req.reply != nil {
		req.reply <- reply[T]{err: err}
	}
}

func (a *ContextActor[T]) operationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, a.opTimeout)
}

func (a *ContextActor[T]) appendToStore(items []T) error {
	ctx, cancel := a.operationContext()
	err := a.store.Append(ctx, items)
	cancel()
	if err == nil {
		a.stored += len(items)
	}
	return err
}

// ── 外部消息面（无锁，仅 channel 进出）──────────────────────────────

// Append 写路径：追加上下文（② 后端写操作）。非阻塞投递；mailbox 满时
// 返回 ErrMailboxFull（调用方可选择同步 flush 或丢弃计数）。
func (a *ContextActor[T]) Append(items []T) error {
	return a.Enqueue(request[T]{op: opAppend, items: items})
}

// LoadWindow 读路径（① 前端 select / ③ 递 LLM）：按窗口读，返回
// [offset, offset+limit) 与总数。同步等待 actor 回复。
func (a *ContextActor[T]) LoadWindow(ctx context.Context, offset, limit int) ([]T, int, error) {
	replyCh := make(chan reply[T], 1)
	if err := a.Enqueue(request[T]{op: opLoadWindow, offset: offset, limit: limit, reply: replyCh}); err != nil {
		return nil, 0, err
	}
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case resp := <-replyCh:
		return resp.items, resp.total, resp.err
	}
}

// Snapshot 返回常驻区拷贝与总数（测试/审计；非生产热路径）。
func (a *ContextActor[T]) Snapshot() ([]T, int) {
	ctx, cancel := context.WithTimeout(context.Background(), a.opTimeout)
	defer cancel()
	items, total, err := a.SnapshotContext(ctx)
	if err != nil {
		return nil, 0
	}
	return items, total
}

// SnapshotContext is a bounded snapshot request. It never spins when the
// mailbox is full and it observes the caller's cancellation while waiting for
// a reply or concurrent Close drain.
func (a *ContextActor[T]) SnapshotContext(ctx context.Context) ([]T, int, error) {
	replyCh := make(chan reply[T], 1)
	if err := a.Enqueue(request[T]{op: opSnapshot, reply: replyCh}); err != nil {
		if !a.closedFlag.Load() {
			return nil, 0, err
		}
		select {
		case <-a.closed:
			return append([]T(nil), a.resident...), len(a.resident) + a.stored, nil
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case resp := <-replyCh:
		return resp.items, resp.total, resp.err
	}
}

// Enqueue 投递消息（非阻塞；满则返回 ErrMailboxFull 并计入丢弃计数；
// Close 完成后显式拒绝——避免 ctx.Done 与 mailbox 同时 ready 的 select 竞态）。
func (a *ContextActor[T]) Enqueue(req request[T]) error {
	a.gate.RLock()
	defer a.gate.RUnlock()
	if a.closedFlag.Load() {
		return fmt.Errorf("lifecycle: actor is closed")
	}
	select {
	case a.mailbox <- req:
		return nil
	default:
		a.drop.Add(1)
		return ErrMailboxFull
	}
}

// ErrMailboxFull 是背压信号：mailbox 已满，投递被丢弃（调用方应聚合重试
// 或同步路径）。流式场景由 BatchPipeline 处理（聚合 flush）。
var ErrMailboxFull = fmt.Errorf("lifecycle: actor mailbox full")

// CloseContext first cancels in-flight Storage work, then closes admission and
// drains every accepted request. A caller may bound its own wait without
// re-opening the actor or leaving a send-on-closed race.
func (a *ContextActor[T]) CloseContext(ctx context.Context) error {
	a.closeOnce.Do(func() {
		a.gate.Lock()
		a.closedFlag.Store(true)
		a.cancel()
		close(a.mailbox)
		a.gate.Unlock()
	})
	select {
	case <-a.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close is the compatibility shutdown API. Use CloseContext when the caller
// is on a UI shutdown path and needs a bounded wait.
func (a *ContextActor[T]) Close() {
	_ = a.CloseContext(context.Background())
}

// Dropped 返回背压丢弃计数（审计）。
func (a *ContextActor[T]) Dropped() int { return int(a.drop.Load()) }
