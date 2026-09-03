// Package session 的端口契约层（9.1）：EnginePort 与 SessionUnit 骨架。
// 会话性归 Seele（seelebridge 实现 EnginePort），seelex 只做薄封装；生命周期
// 热/冷判定由 HasSession 驱动，本包不重造 COLD/PREPARED/LIVE 状态机
// （thin-wrapper-session-design.md §3.2、mbd-models.md §2）。会话粒度存储
// 契约不再经本包暴露（StorePort 已删除：适配职责由 internal/adapters 承担，
// 消费端口定义在 application/core/session_runtime/ports.go）。
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	seelesession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 会话粒度类型以 sessionstore 为单一事实源（存储模块定义，端口层别名；
// sessionstore 不得反向依赖本包，见 session_granular.go 说明）。
type (
	SessionKind     = sessionstore.Kind
	SessionStatus   = sessionstore.Status
	SessionRecord   = sessionstore.Record
	TranscriptEvent = sessionstore.TranscriptEvent
	ToolResultRef   = sessionstore.ToolResultRef
	ContextStack    = sessionstore.ContextStack
	SessionInfo     = sessionstore.SessionInfo
	SessionBinding  = sessionstore.Binding
)

const (
	KindMain      = sessionstore.KindMain
	KindSubagent  = sessionstore.KindSubagent
	StatusDraft   = sessionstore.StatusDraft
	StatusIdle    = sessionstore.StatusIdle
	StatusRunning = sessionstore.StatusRunning
	StatusQueued  = sessionstore.StatusQueued
)

// EngineHandle 是引擎/loop 句柄（E_i，opaque：seelebridge bundle 持有
// Seele Session，seelex 只经 EnginePort 消费，不直接触碰）。
type EngineHandle any

// QueuedInput 是输入队列元素（Q_i）：seq 有序，Payload 不透明（执行内核
// 的 chatRequest），保持会话域解耦。
type QueuedInput struct {
	Seq     uint64
	Text    string
	Payload any
}

// InputQueue 是会话输入队列（Q_i）：seq 递增、线程隔离、只属本会话。
type InputQueue struct {
	mu    sync.Mutex
	seq   uint64
	items []QueuedInput
}

// NewInputQueue 构造空输入队列。
func NewInputQueue() *InputQueue {
	return &InputQueue{}
}

// Enqueue 追加一条输入并分配递增 seq。
func (queue *InputQueue) Enqueue(text string, payload any) QueuedInput {
	if queue == nil {
		return QueuedInput{} // nil 句柄：不 panic、丢弃输入（B1）
	}
	queue.mu.Lock()
	queue.seq++
	item := QueuedInput{Seq: queue.seq, Text: text, Payload: payload}
	queue.items = append(queue.items, item)
	queue.mu.Unlock()
	return item
}

// Dequeue 弹出队首输入；空队列返回 false。
func (queue *InputQueue) Dequeue() (QueuedInput, bool) {
	if queue == nil {
		return QueuedInput{}, false
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.items) == 0 {
		return QueuedInput{}, false
	}
	item := queue.items[0]
	queue.items = queue.items[1:]
	return item, true
}

// Clear 清空队列（幂等重建 Q_i 用）。
func (queue *InputQueue) Clear() {
	if queue == nil {
		return
	}
	queue.mu.Lock()
	queue.items = nil
	queue.mu.Unlock()
}

// Len 返回队列长度。
func (queue *InputQueue) Len() int {
	if queue == nil {
		return 0
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return len(queue.items)
}

// Snapshot 返回队列只读拷贝（seq 有序）。
func (queue *InputQueue) Snapshot() []QueuedInput {
	if queue == nil {
		return nil
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return append([]QueuedInput(nil), queue.items...)
}

// EnginePort 是引擎/loop 能力端口（会话性归 Seele，seelebridge 实现）。
// 热 = HasSession(id) 为真（bundle 存活，attach 视图即可）；冷 =
// NewMainSessionWithID/NewSubagentSessionWithID 重建 bundle 并 seed。
type EnginePort interface {
	// HasSession 报告目标会话引擎是否已实例化（热/冷判定）。
	HasSession(sessionID string) bool
	// NewMainSessionWithID 以显式会话 ID 重建主会话 bundle。
	NewMainSessionWithID(sessionID string, hooks *seelesession.LoopHooks) (*seelesession.Session, error)
	// NewSubagentSessionWithID 以显式会话 ID 重建子代理会话 bundle
	// （plan 节点 = 独立 SessionUnit，own loop/视图/历史）。
	NewSubagentSessionWithID(sessionID string, hooks *seelesession.LoopHooks) (*seelesession.Session, error)
	// ChatStreamFor 向指定会话引擎提交一次流式对话（显式 sid 路由，
	// 防止切换串写）。
	ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)
	// UnloadSession 释放指定会话的引擎实例（unload 后重开走 cold_load）。
	UnloadSession(sessionID string) error
	// PrepareMainSessionHistory 把应用装配的 provider history 交给目标
	// 会话的 DurableHistory（下次 ChatStream 装载）。
	PrepareMainSessionHistory(sessionID string, messages []types.Message) bool
}

// SessionMetaPort 是会话端口的可选扩展：读写会话展示元数据（置顶/别名/排序位，
// 持久化在项目级 meta blob）。未实现时应用层返回明确错误，目录照常枚举（元数据
// 取零值），因此测试桩与最小宿主不受影响。
type SessionMetaPort interface {
	SetSessionMeta(sessionID string, meta model.SessionMeta) error
	SessionMeta(sessionID string) (model.SessionMeta, error)
}

// SessionUnit 是会话资源单元骨架：S_i=(id,K,parent,E,V,Q,C,B,status)。
// 热/冷由 EnginePort.HasSession 判定（loaded），薄状态机只维护可见状态。
type SessionUnit struct {
	ID       string
	Kind     SessionKind
	ParentID string
	Title    string

	mu     sync.Mutex
	status SessionStatus
	loaded bool // 引擎热/冷判定（HasSession 驱动）

	// Runtime 是该会话的运行时投影槽（G1：每会话一份）。进程级只读原件
	// （model/plugins/accounts/...）在 G3 分型前先整份拷贝进槽，之后随
	// SessionSnapshot/ProcessSnapshot 分家只存会话专属字段。
	Runtime model.RuntimeState
	// Revision 是该会话的快照修订号（INV-G5：与进程 revision 互不相干；
	// 会话事件推进自己的 revision，不碰 Snapshot.Revision）。
	Revision uint64

	// Composer 是该会话的未发送输入草稿（G4 先行：草稿会话早分配 SID，
	// Composer 随 record 持久化、跨重启恢复；提交成功后清空）。
	Composer model.ComposerDraft
	// Effort 是该会话选择的 effort 级别（G4：归属进 Unit；未选择时为空，
	// 回退进程级默认 effortManager.Current）。
	Effort string
	// FullAccess 是该会话的全权模式（G4：归属进 Unit；未选择时回退进程
	// 默认/引擎门值——chat 起点按目标会话的生效模式同步门，后台会话不
	// 继承别的会话的遗留开关）。
	FullAccess    bool
	fullAccessSet bool

	// 聊天运行态（9.5 收口：原 ChatRuntime 平行容器已删除，直接收进单元；
	// 执行态归 Seele loop，这里只留 seelex 侧的投影/取消/流/队列桥）。
	Chat    model.ChatState
	Cancel  context.CancelFunc
	Stream  VisibleOutputSink
	Batcher StreamBatcherSink

	Engine  EngineHandle
	View    *View
	Queue   *InputQueue
	Context *ContextStack
	Binding SessionBinding
}

// NewSessionUnit 构造会话单元骨架；空 ID 拒绝（B2 边界）。
func NewSessionUnit(id string, opts ...func(*SessionUnit)) (*SessionUnit, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("session: session ID is required")
	}
	unit := &SessionUnit{
		ID:      strings.TrimSpace(id),
		Kind:    KindMain,
		status:  StatusDraft,
		Chat:    model.ChatState{},
		View:    &View{},
		Queue:   NewInputQueue(),
		Context: &ContextStack{},
	}
	for _, apply := range opts {
		if apply != nil {
			apply(unit)
		}
	}
	return unit, nil
}

// NewDraftUnit 构造草稿槽会话单元（ID 为空：未物化的新会话占位；不参与
// 持久化与引擎绑定，物化时经 NewSessionUnit 重建为真实会话）。
func NewDraftUnit() *SessionUnit {
	return &SessionUnit{
		Kind:    KindMain,
		status:  StatusDraft,
		Chat:    model.ChatState{},
		View:    &View{},
		Queue:   NewInputQueue(),
		Context: &ContextStack{},
	}
}

// WithKind 设置会话种类（main/subagent）。
func WithKind(kind SessionKind) func(*SessionUnit) {
	return func(unit *SessionUnit) {
		unit.Kind = kind
		unit.Binding.Kind = kind
	}
}

// WithParent 设置子代理归属主会话（main 为空）。
func WithParent(parentID string) func(*SessionUnit) {
	return func(unit *SessionUnit) {
		unit.ParentID = parentID
		unit.Binding.ParentID = parentID
	}
}

// WithTitle 设置会话标题。
func WithTitle(title string) func(*SessionUnit) {
	return func(unit *SessionUnit) {
		unit.Title = title
	}
}

// Status 返回当前可见状态。
func (unit *SessionUnit) Status() SessionStatus {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.status
}

// HasSession 返回引擎热/冷判定（loaded 由 ColdLoad/Unload 驱动，与
// EnginePort.HasSession 语义一致：热 = bundle 存活）。
func (unit *SessionUnit) HasSession() bool {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.loaded
}

// ColdLoad 冷加载：cold → idle（要求引擎已创建，hasSession=true）。
// 已加载会话重复冷加载拒绝（B2 双 cold load）。
func (unit *SessionUnit) ColdLoad(hasSession bool) error {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	if unit.loaded {
		return fmt.Errorf("session: %q double cold load rejected", unit.ID)
	}
	if !hasSession {
		return fmt.Errorf("session: %q cold load requires an alive engine", unit.ID)
	}
	unit.loaded = true
	if unit.status == StatusDraft {
		unit.status = StatusIdle
	}
	return nil
}

// Submit 提交输入：idle → running；running/queued 保持（入队由 Queue 承担）。
// 未加载（hasSession=false）提交拒绝（B2）。
func (unit *SessionUnit) Submit(hasSession bool) error {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	if !unit.loaded || !hasSession {
		return fmt.Errorf("session: %q submit requires a loaded engine", unit.ID)
	}
	switch unit.status {
	case StatusIdle:
		unit.status = StatusRunning
		return nil
	case StatusRunning, StatusQueued:
		unit.status = StatusQueued
		return nil
	default:
		return fmt.Errorf("session: %q illegal submit from %q", unit.ID, unit.status)
	}
}

// Finish 完成/取消：running/queued → idle。
func (unit *SessionUnit) Finish() error {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	if unit.status != StatusRunning && unit.status != StatusQueued {
		return fmt.Errorf("session: %q illegal finish from %q", unit.ID, unit.status)
	}
	unit.status = StatusIdle
	return nil
}

// Unload 卸载：释放引擎（loaded=false，回到 draft）。运行中卸载拒绝。
func (unit *SessionUnit) Unload() error {
	unit.mu.Lock()
	defer unit.mu.Unlock()
	if unit.status == StatusRunning || unit.status == StatusQueued {
		return fmt.Errorf("session: %q cannot unload while running", unit.ID)
	}
	if !unit.loaded {
		return nil // 幂等：未加载会话卸载无操作
	}
	unit.loaded = false
	unit.status = StatusDraft
	return nil
}
