package core

import (
	"context"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/worktable"
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

	// sessionChat 是会话级聊天运行态注册表（Core.Mu 保护）。M1 起聊天
	// 保护从全局单例收窄为会话级：同会话串行 + 每会话独立队列/取消。
	sessionChat map[string]*sessionChatRuntime

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
	streamOutput  *chat.VisibleOutputStream
	streamBatcher *chat.StreamBatcher
}

// workTableRuntimeState 持有工作表格增量发布器（CSP 汇聚；见
// worktable_publisher.go）。
type workTableRuntimeState struct {
	workTablePublisher *worktable.WorkTablePublisher
}

type lifecycleRuntimeState struct {
	cancelChat context.CancelFunc
	idle       chan struct{}
	draining   bool
	closed     bool
	// CSP 生命周期消费者（子代理树信号 / plan 节点事件 / task 变更）停止
	// 控制：取代同步回调嵌套，数据经 channel 流转。
	lifecycleStop chan struct{}
	lifecycleOnce sync.Once
	// inputQueue 是会话运行期间排队输入的单一队列（单一写入点：
	// submitConversation；单一消费点：runChat 结尾）。队列输入在每轮
	// ReAct 结束（Session-backed 引擎 OnIterationComplete 返回 false）后由
	// runChat 结尾 drain 并开启下一轮——不设中间提升队列。
	inputQueue []chatRequest
}

type promptRuntimeState struct {
	promptStack   *PromptStack
	effortManager *EffortManager
}
