package core

import (
	"context"
	"sync"
)

// serviceState is assembled from cohesive state groups. Components share the
// application lock where workflows must publish one coherent snapshot, while
// each group still makes ownership and reset boundaries explicit.
type serviceState struct {
	mu sync.RWMutex

	infrastructureState
	conversationRuntimeState
	lifecycleRuntimeState
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
	snapshot     Snapshot
	messageSeq   uint64
	streamOutput *visibleOutputStream
}

type lifecycleRuntimeState struct {
	cancelChat context.CancelFunc
	idle       chan struct{}
	draining   bool
	closed     bool
	inputQueue []chatRequest
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
