// Package session 是 Seelex 的会话域：会话资源（身份、可见投影、聊天运行态、
// 生命周期状态机）的唯一所有者。执行内核（application/core）经本包暴露的端口
// 读写会话；会话之间零共享，继承只走深拷贝。
//
// 本包禁止 import application/core 的实现包（chat 流类型经接口收纳，队列载荷
// 不透明化），确保依赖方向单向：core → session → sessionstore/workspace。
package session

import (
	"context"
	"errors"
	"sync"

	"github.com/RedHuang-0622/seelex/application/model"
)

// LifecycleState 是会话生命周期状态。
type LifecycleState string

const (
	// StateCold 仅持久化，无内存单元（可由目录列出）。
	StateCold LifecycleState = "cold"
	// StatePrepared 冷加载构造中，未发布（冷加载的原子窗口期）。
	StatePrepared LifecycleState = "prepared"
	// StateLive 已驻留（idle 或 fg/bg 运行）。
	StateLive LifecycleState = "live"
)

// VisibleOutputSink 是流式可见输出接收器（由执行内核的 stream 类型实现）。
type VisibleOutputSink interface {
	Consume(chunk string) string
	RequestID() string
}

// StreamBatcherSink 是流式批处理接收器（由执行内核的 batcher 类型实现）。
type StreamBatcherSink interface {
	OnChunk(chunk string)
	FlushPending() error
}

// QueuedRequest 是排队输入：DisplayInput 供会话域投影 ChatState.InputQueue，
// Payload 为不透明载荷（执行内核的 chatRequest），保持会话域解耦。
type QueuedRequest struct {
	DisplayInput string
	Payload      any
}

// ChatRuntime 是单会话聊天运行态（会话域独占；执行内核经端口读写）。
type ChatRuntime struct {
	mu      sync.Mutex
	Chat    model.ChatState
	Cancel  context.CancelFunc
	Queue   []QueuedRequest
	Stream  VisibleOutputSink
	Batcher StreamBatcherSink
}

// ChatState 返回当前聊天可见状态。
func (runtime *ChatRuntime) ChatState() model.ChatState {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.Chat
}

// SetChatState 写入聊天可见状态并同步到所属 Unit 的视图镜像。
func (runtime *ChatRuntime) SetChatState(chat model.ChatState, view *View) {
	runtime.mu.Lock()
	runtime.Chat = chat
	runtime.mu.Unlock()
	if view != nil {
		view.Chat = chat
	}
}

// UpdateChat 在私有锁内应用变更并同步视图镜像（执行内核热路径用）。
func (runtime *ChatRuntime) UpdateChat(mutate func(*model.ChatState), view *View) {
	runtime.mu.Lock()
	if mutate != nil {
		mutate(&runtime.Chat)
	}
	chat := runtime.Chat
	runtime.mu.Unlock()
	if view != nil {
		view.Chat = chat
	}
}

// SetCancel 设置会话取消函数。
func (runtime *ChatRuntime) SetCancel(cancel context.CancelFunc) {
	runtime.mu.Lock()
	runtime.Cancel = cancel
	runtime.mu.Unlock()
}

// CancelFunc 返回会话取消函数（可能为 nil）。
func (runtime *ChatRuntime) CancelFunc() context.CancelFunc {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.Cancel
}

// SetStream 设置流式可见输出接收器。
func (runtime *ChatRuntime) SetStream(stream VisibleOutputSink) {
	runtime.mu.Lock()
	runtime.Stream = stream
	runtime.mu.Unlock()
}

// StreamSink 返回流式可见输出接收器（可能为 nil）。
func (runtime *ChatRuntime) StreamSink() VisibleOutputSink {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.Stream
}

// SetBatcher 设置流式批处理接收器。
func (runtime *ChatRuntime) SetBatcher(batcher StreamBatcherSink) {
	runtime.mu.Lock()
	runtime.Batcher = batcher
	runtime.mu.Unlock()
}

// BatcherSink 返回流式批处理接收器（可能为 nil）。
func (runtime *ChatRuntime) BatcherSink() StreamBatcherSink {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.Batcher
}

// PendingRequests 返回排队输入列表（拷贝）。
func (runtime *ChatRuntime) PendingRequests() []QueuedRequest {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return append([]QueuedRequest(nil), runtime.Queue...)
}

// SetRequests 整体替换排队输入。
func (runtime *ChatRuntime) SetRequests(requests []QueuedRequest) {
	runtime.mu.Lock()
	runtime.Queue = append([]QueuedRequest(nil), requests...)
	runtime.mu.Unlock()
}

// Enqueue 追加一条排队输入。
func (runtime *ChatRuntime) Enqueue(request QueuedRequest) {
	runtime.mu.Lock()
	runtime.Queue = append(runtime.Queue, request)
	runtime.mu.Unlock()
}

// View 是单会话可见投影（会话域独占）。
type View struct {
	Conversation       []model.Message
	Chat               model.ChatState
	ReadFiles          []model.ReadFileRef
	TotalMessages      int
	HistoryOffset      int
	HasMoreHistory     bool
	ConversationWindow int
	Revision           uint64
}

// Unit 是会话资源单元：每个会话一个，持有私有锁；同一时刻只有一个执行内核
// 的写者经端口访问，会话之间互不阻塞（线程隔离）。
type Unit struct {
	ID    string
	mu    sync.Mutex
	state LifecycleState
	View  *View
	Chat  *ChatRuntime
}

// Lifecycle 返回当前生命周期状态。
func (unit *Unit) Lifecycle() LifecycleState {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.state
}

// Transition 执行生命周期迁移；非法迁移返回错误（状态机硬约束）。
func (unit *Unit) Transition(next LifecycleState) error {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	if err := validTransition(unit.state, next); err != nil {
		return err
	}
	unit.state = next
	return nil
}

// validTransition 定义状态机迁移表（COLD→PREPARED→LIVE；LIVE↔LIVE 允许）。
func validTransition(from, to LifecycleState) error {
	switch from {
	case StateCold:
		if to == StatePrepared {
			return nil
		}
	case StatePrepared:
		if to == StateLive {
			return nil
		}
	case StateLive:
		if to == StateLive || to == StateCold {
			return nil
		}
	}
	return errors.New("session: illegal lifecycle transition")
}

// Domain 是会话域：会话资源注册表 + 当前视图指针 V 的所有者。
// V 是 core 允许持有的唯一会话引用（视图指针），见详细设计 1.1。
type Domain struct {
	mu       sync.RWMutex
	units    map[string]*Unit
	activeID string
}

// NewDomain 构造空会话域。
func NewDomain() *Domain {
	return &Domain{units: make(map[string]*Unit)}
}

// Register 注册一个会话单元（COLD → 构造后 PREPARED 由调用方迁移）。
func (domain *Domain) Register(unit *Unit) {
	if unit == nil {
		return
	}
	domain.mu.Lock()
	domain.units[unit.ID] = unit
	domain.mu.Unlock()
}

// Unit 返回指定会话单元；不存在时返回 nil。
func (domain *Domain) Unit(sid string) *Unit {
	domain.mu.RLock()
	unit := domain.units[sid]
	domain.mu.RUnlock()
	return unit
}

// Remove 注销会话单元（unload 后调用）。
func (domain *Domain) Remove(sid string) {
	domain.mu.Lock()
	delete(domain.units, sid)
	domain.mu.Unlock()
}

// SetActive 移动当前视图指针 V（hot_attach / cold_load 发布后调用）。
func (domain *Domain) SetActive(sid string) {
	domain.mu.Lock()
	domain.activeID = sid
	domain.mu.Unlock()
}

// ActiveID 返回当前视图指针指向的会话 ID。
func (domain *Domain) ActiveID() string {
	domain.mu.RLock()
	defer domain.mu.RUnlock()
	return domain.activeID
}

// Live 返回当前驻留会话数（测试与监控用）。
func (domain *Domain) Live() int {
	domain.mu.RLock()
	defer domain.mu.RUnlock()
	return len(domain.units)
}

// UnitIDs 返回全部驻留会话 ID（排序由调用方负责）。
func (domain *Domain) UnitIDs() []string {
	domain.mu.RLock()
	defer domain.mu.RUnlock()
	ids := make([]string, 0, len(domain.units))
	for sid := range domain.units {
		ids = append(ids, sid)
	}
	return ids
}

// NewUnit 构造会话单元（ID 非空；COLD 态）。
func NewUnit(sid string) *Unit {
	return &Unit{
		ID:    sid,
		state: StateCold,
		View:  &View{},
		Chat:  &ChatRuntime{},
	}
}
