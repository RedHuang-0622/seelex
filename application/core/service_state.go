package core

import (
	"context"
	"sync"
	"sync/atomic"
)

// serviceState is assembled from cohesive state groups. Components share the
// application lock where workflows must publish one coherent snapshot, while
// each group still makes ownership and reset boundaries explicit.
type serviceState struct {
	mu sync.RWMutex

	infrastructureState
	conversationRuntimeState
	lifecycleRuntimeState
	workTableRuntimeState
	promptRuntimeState
	sessionRuntimeState
	planRuntimeState
	taskRuntimeState
}

type infrastructureState struct {
	deps     Dependencies
	events   *EventHub
	approval *ApprovalBroker
	commands *CommandRegistry
}

type conversationRuntimeState struct {
	snapshot      Snapshot
	messageSeq    uint64
	streamOutput  *visibleOutputStream
	streamBatcher *StreamBatcher
}

// workTableRuntimeState 持有工作表格增量发布器（CSP 汇聚；见
// worktable_publisher.go）。
type workTableRuntimeState struct {
	workTablePublisher *workTablePublisher
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
	inputQueue    []chatRequest
	// deferredInputQueue has been acknowledged to the UI after a framework
	// loop returns, but remains available to the current turn's persistence
	// and is promoted into the next turn only after that persistence boundary.
	deferredInputQueue []chatRequest
	// pendingSubagentContexts 是子代理 merge-back 排队内容：节点执行期间主
	// 会话被 ChatStream 持锁，回传只能排队；下一次 startChat（锁外）注入
	// engine history。
	pendingSubagentContexts []string
}

type promptRuntimeState struct {
	promptStack   *PromptStack
	effortManager *EffortManager
}

type sessionRuntimeState struct {
	sessionNameMu       sync.Mutex
	sessionTransitionMu sync.Mutex
	sessionNames        map[string]sessionNameCacheEntry
	sessionTitle        SessionTitle
	// session catalog I/O is owned by a dedicated refresh worker. Snapshot
	// reads only snapshot.Sessions, never SessionPort/WorkspacePort.
	sessionCatalogWake chan struct{}
	sessionCatalogStop chan struct{}
	sessionCatalogDone chan struct{}
	sessionCatalogOnce sync.Once
}

type planRuntimeState struct {
	planStack      []SessionPlanFrame
	activePlanID   string
	planSequence   uint64
	replanInFlight map[string]struct{}
	reactBudget    *activeReActBudget
}

type taskRuntimeState struct {
	taskExecution           *taskExecutionState
	taskService             *TaskService // 当前任务的 TaskService（与 taskExecution 同生命周期）
	goalSkillActive         atomic.Bool
	contextControlFailure   error
	contextControlRequestID string
	tokenCounter            requestTokenCounter
	transcript              []TranscriptEvent
	transcriptSeq           uint64
	pendingProviderCalls    []TranscriptToolCall
	pendingToolResults      []StoredToolResult
	toolResultRefs          []ToolResultRef
	resultRefsByToolCallID  map[string]string
	taskCheckpoints         []TaskCheckpoint
}
