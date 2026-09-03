package core

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/worktable"
	"github.com/RedHuang-0622/seelex/session"
)

// serviceState is assembled from cohesive state groups. Components share the
// application lock (Core.ViewMu) where workflows must publish one coherent
// snapshot; the authoritative Snapshot and external ports live in the shared
// state kernel (Core), while each group still makes ownership and reset
// boundaries explicit.
type serviceState struct {
	*state.Core
	commands *CommandRegistry

	conversationRuntimeState
	lifecycleRuntimeState
	workTableRuntimeState
	promptRuntimeState

	// sessions 是会话域（阶段 B：会话资源唯一所有者；本状态只保留当前
	// 会话的只读视图指针 V，不再持有任何会话容器）。
	sessions *session.Domain

	// draft 是"新建会话"草稿槽位（Core.ViewMu 保护）：早分配 SID 的草稿
	// （HasSession=false，不建引擎 bundle）持有真实会话 ID 与工作区绑定；
	// 首次提交（materializeDraftSession）时消费并清空。
	draft *draftSlot

	// chatSeq 是聊天请求 ID 的单调序号（Core.ViewMu 保护）。requestID 必须
	// 跨会话唯一：Windows 上 time.Now().UnixNano() 分辨率约 0.5ms，并行
	// 会话在同一 tick 启动会碰撞，导致 request→session 绑定与
	// ClearReActBudget/FinalizeTask 串写。附加序号消除碰撞。
	chatSeq uint64
	// draftSeq 是草稿会话 ID 的单调序号（Core.ViewMu 保护）：早分配 SID 在
	// Windows 时间戳低分辨率下也保持同 tick 内唯一。
	draftSeq uint64
	// sessionIDSeq 是显式新建会话（fork/切项目）ID 的全局原子序号
	// （F-4：逐会话宿主不再经 engine.StartSession 拿活跃别名 ID）。
	sessionIDSeq atomic.Uint64
	// residentOrder 是驻留引擎（session bundle）的 LRU 使用序（Core.ViewMu
	// 保护；索引 0 = 最近使用）。G6 驱逐按最旧优先；running/queued/
	// awaiting_approval 与当前视图会话不可驱逐（INV-G8）。
	residentOrder []string

	// fullAccessDefault 是进程级全权默认（装配期从引擎门捕获一次；G4：
	// 会话未选择时回退该值，不继承其它会话的遗留开关）。
	fullAccessDefault bool
}

// draftSlot 保留草稿状态。ID 是早分配的真实会话 ID（草稿会话键）；
// Workspace 为"工作区会话"草稿的工作区绑定（任务会话草稿为 nil）。
type draftSlot struct {
	ID        string
	Workspace *WorkspaceInfo
	CreatedAt time.Time
	UpdatedAt time.Time
}

type conversationRuntimeState struct {
}

// workTableRuntimeState 持有工作表格增量发布器（CSP 汇聚；见
// worktable_publisher.go）。
type workTableRuntimeState struct {
	workTablePublisher *worktable.WorkTablePublisher
}

type lifecycleRuntimeState struct {
	idle     chan struct{}
	draining bool
	closed   bool
	// CSP 生命周期消费者（子代理树信号 / plan 节点事件 / task 变更）停止
	// 控制：取代同步回调嵌套，数据经 channel 流转。
	lifecycleStop chan struct{}
	lifecycleOnce sync.Once
}

type promptRuntimeState struct {
	promptStack   *PromptStack
	effortManager *EffortManager
}
