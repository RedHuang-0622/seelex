package core

import (
	"sync"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/backend/engine/prompt"
	"github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/middle/view/board"
	"github.com/RedHuang-0622/seelex/middle/view/state"
	"github.com/RedHuang-0622/seelex/middle/view/subagent"
)

// serviceComponents is the application composition graph. Service coordinates
// these parts but does not own their implementation details.
type serviceComponents struct {
	prompts  *prompt_layer.Coordinator
	context  *context_runtime.Coordinator
	history  *context_runtime.HistoryCoordinator
	sessions *session_runtime.Coordinator
	tasks    *task_context.Coordinator
	// goal 是会话级 goal 治理协调器（P1：每会话 Controller+Supervisor；
	// 视图投影/工具路由经 Service 面暴露）。
	goal     *goalCoordinator
	view     *view_state.Coordinator
	subagent *subagent_view.Coordinator
	input    inputDispatcher

	// projectBoard 是装配期注入的**可选写侧端口**（项目看板投影的订阅口，M3）。
	//
	// 为什么落在这里而不是 facade 的第三个字段：facade 只允许 state + components
	// （TestServiceFacadeContainsOnlyAssembly 就是这条结构断言的守门人），而它是
	// 装配期交接的外部端口——与其它组件同一生态位（注入一次、此后只读）。
	//
	// 为什么不落进某个 serviceState 状态组：状态组是**用例会读改的会话状态**，
	// 放进去等于给"第二份判定/第二份事实"开口子（本能力只需要一个转发口）。
	//
	// 为什么是**指针**槽位：serviceComponents 是可复制/可 `%+v` 的纯装配图
	// （既有用例与 `go vet` 都这么用它）；内联 mutex 会让整张图变成不可复制
	// （vet: "copies lock value"），指针让"可变性"只留在槽位自己身上。
	projectBoard *projectBoardPort

	// toolRecord 是工具记录台账的可变槽位（可选端口 + 计数开关，见 tool_record.go）。
	// 与 projectBoard 同一生态位与同一套锁形状：装配期交接一次、此后每个工具完成读一份。
	toolRecord *toolRecordPort
}

// toolRecordPort 是**计数开关 + 落盘错误**的可变槽位（读数本身按会话存在
// serviceState.toolRecords 里，见 tool_record.go）。
//
// 开关与错误读数放同一把锁：它们回答的是同一件事（"现在记不记、最近一次落盘成没成"）。
type toolRecordPort struct {
	mu           sync.RWMutex
	enabled      bool
	lastFlushErr error
	// snapshotStore 是读数落盘口（会话粒度、批次更新，见 tool_record.go 的
	// FlushToolRecord）。装配根在 New 之后注入（SetToolRecordSnapshotStore）：
	// 它依赖存储后端与工作区解析，两者在装配顺序上都晚于 core 的构造。
	snapshotStore ToolRecordSnapshotStore
}

func (port *toolRecordPort) setEnabled(enabled bool) {
	port.mu.Lock()
	port.enabled = enabled
	port.mu.Unlock()
}

func (port *toolRecordPort) enabledValue() bool {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.enabled
}

func (port *toolRecordPort) noteFlushError(err error) {
	port.mu.Lock()
	port.lastFlushErr = err
	port.mu.Unlock()
}

func (port *toolRecordPort) flushErrorMessage() string {
	port.mu.RLock()
	defer port.mu.RUnlock()
	if port.lastFlushErr == nil {
		return ""
	}
	return port.lastFlushErr.Error()
}

func (port *toolRecordPort) setSnapshotStore(store ToolRecordSnapshotStore) {
	port.mu.Lock()
	port.snapshotStore = store
	port.mu.Unlock()
}

func (port *toolRecordPort) snapshotStoreValue() ToolRecordSnapshotStore {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.snapshotStore
}

// projectBoardPort 是项目看板订阅口（board.ProjectBoardSink）的**可变槽位**：
// 装配期交接一次、此后每回合读一份（读多写一，单独一把锁，不碰 Core.ViewMu）。
type projectBoardPort struct {
	mu   sync.RWMutex
	sink board.ProjectBoardSink
}

func (port *projectBoardPort) store(sink board.ProjectBoardSink) {
	port.mu.Lock()
	port.sink = sink
	port.mu.Unlock()
}

func (port *projectBoardPort) load() board.ProjectBoardSink {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.sink
}
