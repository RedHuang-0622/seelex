package core

import (
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/worktable"
	"github.com/RedHuang-0622/seelex/session"
)

// serviceState is assembled from cohesive state groups. Components share the
// application lock (Core.Mu) where workflows must publish one coherent
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
	planProjectionState

	// sessions 是会话域（阶段 B：会话资源唯一所有者；本状态只保留当前
	// 会话的只读视图指针 V，不再持有任何会话容器）。
	sessions *session.Domain

	// draft 是"新建会话"草稿槽位（Core.Mu 保护）：草稿没有真实会话 ID、
	// 不落盘，但切换会话后仍保留并可恢复；工作区会话草稿同时保留工作区
	// 绑定。首次提交（materializeDraftSession）时消费并清空。
	draft *draftSlot

	// chatSeq 是聊天请求 ID 的单调序号（Core.Mu 保护）。requestID 必须
	// 跨会话唯一：Windows 上 time.Now().UnixNano() 分辨率约 0.5ms，并行
	// 会话在同一 tick 启动会碰撞，导致 request→session 绑定与
	// ClearReActBudget/FinalizeTask 串写。附加序号消除碰撞。
	chatSeq uint64
}

// draftSlot 保留草稿状态。Workspace 为"工作区会话"草稿的工作区绑定
// （任务会话草稿为 nil）。
type draftSlot struct {
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

// planProjections 是 per-session plan 显示投影缓存（Core.Mu 保护）：当前
// 会话的投影与 Snapshot.Runtime.Plan 同一指针；后台会话的 plan 事件只写
// 自己的投影（P6 收口），切换回看时经 SnapshotOf/sessionActivePlanLocked
// 读取。plan 节点状态属运行期显示态，不落盘（resume 由 plan 帧重建）。
type planProjectionState struct {
	planProjections map[string]*PlanState
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
