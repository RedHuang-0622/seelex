// Package session 是 Seelex 的会话域：会话资源（身份、可见投影、聊天运行态）
// 的唯一所有者。执行内核（application/core）经本包暴露的端口读写会话；
// 会话之间零共享，继承只走深拷贝；热/冷判定由引擎 HasSession 驱动。
//
// 本包禁止 import application/core 的实现包（chat 流类型经接口收纳，队列载荷
// 不透明化），确保依赖方向单向：core → session → sessionstore/workspace。
//
// 9.5 清理：自造 Unit/ChatRuntime 平行容器已删除，聊天运行态（Chat 状态/
// 取消/流/队列）直接收进 SessionUnit，core 直接经 SessionUnit 接入会话域。
package session

import (
	"context"
	"sync"

	"github.com/RedHuang-0622/seelex/application/model"
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

// View 是单会话可见投影（会话域独占）。
type View struct {
	mu                 sync.RWMutex
	Conversation       []model.Message
	Chat               model.ChatState
	ReadFiles          []model.ReadFileRef
	TotalMessages      int
	HistoryOffset      int
	HasMoreHistory     bool
	ConversationWindow int
	Revision           uint64
	// ContentUnloaded 标记本会话的**可见正文已从内存卸载**（application/core
	// 的内容 LRU：视图未切换到的、且不在运行中的会话，其 Conversation 窗口
	// 按上限做 LRU 释放）。置位时 Conversation 必为空，而 TotalMessages /
	// HistoryOffset / HasMoreHistory / ConversationWindow / Chat / ReadFiles 等
	// 窗口标志、统计与会话事实一律保留——磁盘是唯一事实源，再激活或分页时
	// 经冷回读恢复同一窗口位置。
	ContentUnloaded bool
}

// Mutate 在视图私有锁内应用变更（会话写路径；活跃镜像在 Core.ViewMu 下经 Read 克隆）。
func (view *View) Mutate(mutate func(*View)) {
	view.mu.Lock()
	defer view.mu.Unlock()
	if mutate != nil {
		mutate(view)
	}
}

// Read 在视图私有锁（读）内读取投影快照字段。
func (view *View) Read(read func(*View)) {
	view.mu.RLock()
	defer view.mu.RUnlock()
	if read != nil {
		read(view)
	}
}

// Clone 返回视图的深拷贝（镜像/快照用；返回指针避免拷贝内部锁）。
func (view *View) Clone() *View {
	var clone View
	view.Read(func(source *View) {
		clone.Conversation = append([]model.Message(nil), source.Conversation...)
		clone.Chat = source.Chat
		clone.ReadFiles = append([]model.ReadFileRef(nil), source.ReadFiles...)
		clone.TotalMessages = source.TotalMessages
		clone.HistoryOffset = source.HistoryOffset
		clone.HasMoreHistory = source.HasMoreHistory
		clone.ConversationWindow = source.ConversationWindow
		clone.Revision = source.Revision
		clone.ContentUnloaded = source.ContentUnloaded
	})
	return &clone
}

// ── SessionUnit 聊天运行态（原 ChatRuntime 方法，9.5 收口进单元）──────

// ChatState 返回当前聊天可见状态。
func (unit *SessionUnit) ChatState() model.ChatState {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Chat
}

// SetChatState 写入聊天可见状态并同步到单元视图镜像。
func (unit *SessionUnit) SetChatState(chat model.ChatState, view *View) {
	unit.mu.Lock()
	unit.Chat = chat
	unit.mu.Unlock()
	if view != nil {
		view.Chat = chat
	}
}

// UpdateChat 在单元锁内应用变更并同步视图镜像（执行内核热路径用）。
func (unit *SessionUnit) UpdateChat(mutate func(*model.ChatState), view *View) {
	unit.mu.Lock()
	if mutate != nil {
		mutate(&unit.Chat)
	}
	chat := unit.Chat
	unit.mu.Unlock()
	if view != nil {
		view.Chat = chat
	}
}

// SetCancel 设置会话取消函数。
func (unit *SessionUnit) SetCancel(cancel context.CancelFunc) {
	unit.mu.Lock()
	unit.Cancel = cancel
	unit.mu.Unlock()
}

// CancelFunc 返回会话取消函数（可能为 nil）。
func (unit *SessionUnit) CancelFunc() context.CancelFunc {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Cancel
}

// SetStream 设置流式可见输出接收器。
func (unit *SessionUnit) SetStream(stream VisibleOutputSink) {
	unit.mu.Lock()
	unit.Stream = stream
	unit.mu.Unlock()
}

// StreamSink 返回流式可见输出接收器（可能为 nil）。
func (unit *SessionUnit) StreamSink() VisibleOutputSink {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Stream
}

// SetBatcher 设置流式批处理接收器。
func (unit *SessionUnit) SetBatcher(batcher StreamBatcherSink) {
	unit.mu.Lock()
	unit.Batcher = batcher
	unit.mu.Unlock()
}

// BatcherSink 返回流式批处理接收器（可能为 nil）。
func (unit *SessionUnit) BatcherSink() StreamBatcherSink {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Batcher
}

// Enqueue 追加一条排队输入（Q_i：InputQueue 承载，Payload 不透明）。
func (unit *SessionUnit) Enqueue(request QueuedRequest) {
	if unit == nil || unit.Queue == nil {
		return
	}
	unit.Queue.Enqueue(request.DisplayInput, request)
}

// PendingRequests 返回排队输入列表（拷贝；无法还原的载荷跳过）。
func (unit *SessionUnit) PendingRequests() []QueuedRequest {
	if unit == nil || unit.Queue == nil {
		return nil
	}
	out := make([]QueuedRequest, 0, unit.Queue.Len())
	for _, item := range unit.Queue.Snapshot() {
		if request, ok := item.Payload.(QueuedRequest); ok {
			out = append(out, request)
		}
	}
	return out
}

// SetRequests 整体替换排队输入（幂等重建 Q_i）。
func (unit *SessionUnit) SetRequests(requests []QueuedRequest) {
	if unit == nil {
		return
	}
	if unit.Queue == nil {
		unit.Queue = NewInputQueue()
	}
	unit.Queue.Clear()
	for _, request := range requests {
		unit.Enqueue(request)
	}
}

// QueueProjection 返回排队输入的展示投影（逐项文本 + 计数）——它是
// ChatState.InputQueue / QueuedCount 的唯一来源，也是「投影下标空间」的定义：
//
//	投影下标 i == 队列下标 i（逐项、按队列顺序、不按载荷类型过滤）
//
// 之所以由会话域给出（而不是应用层各自拼），是因为编辑操作
// （RecallRequest / ReorderRequests）就作用在队列下标上：只要投影与队列同源
// 同序，"用户看到的第 i 行"就必然等于"被撤回/被移动的第 i 项"。应用层一旦
// 自行过滤投影（例如只保留某种载荷），投影会比队列短，下标就会错位——调换
// 看不见效果、撤回把没显示过的条目交还输入框。
func (unit *SessionUnit) QueueProjection() ([]string, int) {
	if unit == nil || unit.Queue == nil {
		return nil, 0
	}
	snapshot := unit.Queue.Snapshot()
	displays := make([]string, 0, len(snapshot))
	for _, item := range snapshot {
		displays = append(displays, item.Text)
	}
	return displays, len(snapshot)
}

// RecallRequest 移除并返回指定下标的排队输入（撤回编辑：调用方把
// DisplayInput 交还输入框，引擎侧 Payload 一并丢弃）。下标是「投影下标」
// （见 QueueProjection：与队列下标同一套）。下标越界或 nil 单元返回 false；
// 载荷无法还原为 QueuedRequest 时以队列文本兜底重建。
func (unit *SessionUnit) RecallRequest(index int) (QueuedRequest, bool) {
	if unit == nil || unit.Queue == nil {
		return QueuedRequest{}, false
	}
	item, ok := unit.Queue.RemoveAt(index)
	if !ok {
		return QueuedRequest{}, false
	}
	if request, ok := item.Payload.(QueuedRequest); ok {
		return request, true
	}
	return QueuedRequest{DisplayInput: item.Text}, true
}

// ReorderRequests 把「投影下标」from 的排队输入移动到 to（调换排队顺序；
// 下标空间见 QueueProjection：与队列下标同一套，不做载荷过滤）。
// 越界或 nil 单元返回 false；from==to 为成功的空操作。
func (unit *SessionUnit) ReorderRequests(from, to int) bool {
	if unit == nil || unit.Queue == nil {
		return false
	}
	return unit.Queue.Move(from, to)
}
